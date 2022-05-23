// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package aggregation

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gardener/network-problem-detector/pkg/common/nwpd"

	"github.com/sirupsen/logrus"
)

type obsAggr struct {
	log          logrus.FieldLogger
	lock         sync.Mutex
	aggregations map[jobEdge]*jobEdgeAggregation
	reportPeriod time.Duration
	timeWindow   time.Duration
	lastReport   time.Time
}

type jobEdge struct {
	jobID    string
	srcHost  string
	destHost string
}

func (je jobEdge) String() string {
	return fmt.Sprintf("%s->%s[%s]", je.srcHost, je.destHost, je.jobID)
}

type jobEdgeAggregation struct {
	firstTime          time.Time
	totalCount         int
	reportStart        time.Time
	reportOkCount      int
	reportFailureCount int
	okLast             time.Time
	okStrike           int
	failedLast         time.Time
	failedStrike       int
	lastObs            *nwpd.Observation
}

func (jea *jobEdgeAggregation) IsOKSinceLastReport() bool {
	return jea.reportFailureCount == 0
}

func (jea *jobEdgeAggregation) IsLastOK() bool {
	return jea.okStrike > 0
}

func (jea *jobEdgeAggregation) Report(je jobEdge, start time.Time) string {
	if jea.IsOKSinceLastReport() {
		msg := fmt.Sprintf("%s: OK", je)
		if jea.lastObs != nil && jea.lastObs.Duration != nil {
			d := jea.lastObs.Duration.AsDuration().Milliseconds()
			msg += fmt.Sprintf(" (%d ms)", d)
		}
		if jea.lastObs != nil && jea.lastObs.Timestamp.AsTime().Before(start) {
			msg += fmt.Sprintf(" last observed: %s", utctime(jea.lastObs.Timestamp.AsTime()))
		}
		return msg
	}
	seconds := int(time.Now().Sub(jea.reportStart).Seconds())
	return fmt.Sprintf("%s: %d/%d checks failed in last %ds (last ok: %s)", je,
		jea.reportFailureCount, jea.reportFailureCount+jea.reportOkCount, seconds, utctime(jea.okLast))
}

func (jea *jobEdgeAggregation) add(obs *nwpd.Observation) {
	jea.totalCount++
	jea.lastObs = obs
	if obs.Ok {
		if jea.okLast.Before(jea.failedLast) {
			jea.okStrike = 0
		}
		jea.okLast = obs.Timestamp.AsTime()
		jea.okStrike++
		jea.reportOkCount++
	} else {
		if jea.failedLast.Before(jea.okLast) {
			jea.failedStrike = 0
		}
		jea.failedLast = obs.Timestamp.AsTime()
		jea.failedStrike++
		jea.reportFailureCount++
	}
}

func (jea *jobEdgeAggregation) lastTimestamp() time.Time {
	if jea.okStrike > 0 {
		return jea.okLast
	}
	if jea.failedStrike > 0 {
		return jea.failedLast
	}
	return jea.firstTime
}

type groupCounter struct {
	ok      map[string]int
	unknown map[string]int
	failed  map[string]int
}

func newGroupCounter() *groupCounter {
	return &groupCounter{
		ok:      map[string]int{},
		unknown: map[string]int{},
		failed:  map[string]int{},
	}
}

func (c *groupCounter) inc(key string, ok *bool) {
	if ok == nil {
		c.unknown[key] += 1
	} else if *ok {
		c.ok[key] += 1
	} else {
		c.failed[key] += 1
	}
}

func (c *groupCounter) summary() string {
	var failedNames []string
	for key := range c.failed {
		failedNames = append(failedNames, key)
	}
	sort.Strings(failedNames)
	suffix := ""
	if len(failedNames) > 0 {
		suffix = fmt.Sprintf(" (failed items: %s)", strings.Join(failedNames, ","))
	}
	return fmt.Sprintf("ok/unknown/failed: %d/%d/%d%s", len(c.ok), len(c.unknown), len(c.failed), suffix)
}

var _ nwpd.ObservationListener = &obsAggr{}

func NewObsAggregator(log logrus.FieldLogger, reportPeriod, timeWindow time.Duration) *obsAggr {
	return &obsAggr{
		log:          log,
		aggregations: map[jobEdge]*jobEdgeAggregation{},
		lastReport:   time.Now(),
		reportPeriod: reportPeriod,
		timeWindow:   timeWindow,
	}
}

func (a *obsAggr) Add(obs *nwpd.Observation) {
	a.lock.Lock()
	defer a.lock.Unlock()

	je := jobEdge{jobID: obs.JobID, srcHost: obs.SrcHost, destHost: obs.DestHost}
	jea := a.aggregations[je]
	if jea == nil {
		jea = &jobEdgeAggregation{
			firstTime:   obs.Timestamp.AsTime(),
			reportStart: obs.Timestamp.AsTime(),
		}
		a.aggregations[je] = jea
	}

	jea.add(obs)

	if a.lastReport.Add(a.reportPeriod).Before(time.Now()) {
		go a.reportToLog()
		a.lastReport = time.Now()
	}
}

type reportData struct {
	fullReport  bool
	start       time.Time
	end         time.Time
	jobCounter  *groupCounter
	srcCounter  *groupCounter
	destCounter *groupCounter
	noissues    []string
	issues      []string
}

func newReportData(start, end time.Time, fullReport bool) *reportData {
	return &reportData{
		fullReport:  fullReport,
		start:       start,
		end:         end,
		jobCounter:  newGroupCounter(),
		srcCounter:  newGroupCounter(),
		destCounter: newGroupCounter(),
	}
}

func (r *reportData) add(je jobEdge, aggr *jobEdgeAggregation) {
	var ok *bool
	if aggr.reportFailureCount != 0 || aggr.reportOkCount != 0 {
		good := aggr.reportFailureCount == 0
		ok = &good
	}
	r.jobCounter.inc(je.jobID, ok)
	r.srcCounter.inc(je.srcHost, ok)
	r.destCounter.inc(je.destHost, ok)
	if ok != nil && !*ok {
		r.issues = append(r.issues, aggr.Report(je, r.start))
	} else if r.fullReport || ok == nil {
		r.noissues = append(r.noissues, aggr.Report(je, r.start))
	}
}

func (r *reportData) sort() {
	sort.Strings(r.issues)
	sort.Strings(r.noissues)
}

func (r *reportData) summary() []string {
	return []string{
		fmt.Sprintf("Jobs: %s", r.jobCounter.summary()),
		fmt.Sprintf("SourceHost: %s", r.srcCounter.summary()),
		fmt.Sprintf("DestHost: %s", r.destCounter.summary()),
	}
}

func (a *obsAggr) reportToLog() {
	report := a.calcReport(false, true)
	report.sort()
	prefix := "Report: "
	for _, s := range report.noissues {
		a.log.Info(prefix + s)
	}
	for _, s := range report.issues {
		a.log.Warn(prefix + s)
	}
	for _, s := range report.summary() {
		a.log.Info(prefix + s)
	}
}

func (a *obsAggr) calcReport(fullReport, resetCount bool) *reportData {
	a.lock.Lock()
	defer a.lock.Unlock()

	end := time.Now()
	start := end.Add(-1 * a.reportPeriod)
	outdated := end.Add(-1 * a.timeWindow)
	report := newReportData(start, end, fullReport)
	for je, aggr := range a.aggregations {
		if aggr.lastTimestamp().Before(outdated) {
			delete(a.aggregations, je)
		}
		report.add(je, aggr)
		if resetCount {
			aggr.reportOkCount = 0
			aggr.reportFailureCount = 0
		}
	}
	return report
}

func utctime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}
