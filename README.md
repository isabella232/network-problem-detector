# Network Problem Detector POC

[![reuse compliant](https://reuse.software/badge/reuse-compliant.svg)](https://reuse.software/)

The Network Problem Detector performs and collects various checks between all nodes of a Kubernetes cluster, to its Kube API server and/or external endpoints. Checks are performed using TCP connections, PING (ICMP) or mDNS (UDP).

## Summary

An automated mechanism for aggregrating and collecting the the observations (=results of checks) is still missing. For the moment use these steps for applying it to a cluster:

1. Build the `nwpdcli` with

   ```bash
   make build-local
   ```

2. Set the `KUBECONFIG` environment variable to the kubeconfig of the cluster

3. Apply the two daemon sets (one for the node network, one for the pod network) with

   ```bash
   ./nwpdcli deploy agent
   ```

This step will also provide a default configuration with jobs for the daemon sets on node and pod networks. See below for more details.

4. Optional: In a second shell run the controller to update the configuration on changes of nodes and pod endpoints of the pod network daemon set with

   ```bash
   ./nwpdcli run-controller 
   ```

   Alternatively install the agent controller with

   ```bash
   ./nwpdcli deploy controller 
   ```

6. Collect the observations from all nodes with

   ```bash
   ./nwpdcli collect
   ```

7. Aggregate the observations in text or SVG form

   ```bash
   ./nwpdcli aggr --minutes 10 --svg-output aggr.html
   open aggr.html # to open the html on Mac OS
   ```

Your may apply filters on time window, source, destination or job ID to restrict the aggregation. See `./nwpdcli aggr --help` for more details.

7. Optional: Repeat steps 5. and 6. anytime

8. Remove daemon sets with

   ```bash
   ./nwpdcli deploy agent --delete
   ```

9. Optional: Remove controller deployment  with

   ```bash
   ./nwpdcli deploy controller --delete
   ```

## Default Configuration of Check "Jobs"

Checks are defined as "jobs" using virtual command lines. These command lines are just Go routines executed periodically from the agent running in the pods of the two daemon sets.
There are two daemon sets. One running in the node network (i.e. using the host network in the pod), the other one running in the pod network.

### Default configuration

To examine the current default configuration, run the command

```bash
./nwpdcli deploy print-default-config
```

### Job types

1. `checkTCPPort [--period <duration>] [--endpoints <host1:ip1:port1>,<host2:ip2:port2>,... | --endpoints-of-pod-ds | --node-port <port> ]`

   Tries to open a connection to the given `IP:port`. There are three variants:
   - using an explicit list of endpoints with `--endpoints`
   - using the known pod endpoints of the pod network daemon set
   - using a node port on all known nodes

   The checks run in a robin round fashion after an inital random shuffle. The global default period between two checks can overwritten with the `--period` option.

   Note that known nodes and pod endpoints are only updated if the
   daemon sets are deployed with `nwpdcli deploy all` or watched with `nwpdcli deploy watch`. Changes are applied only every 30 seconds and may need some more time to be discovered by the kubelets.

2. `pingHost [--period <duration>] [--hosts <host1:ip1>,<host2:ip2>,...]`

   Robin round ping to all nodes or the provided host list. The  node or host list is shuffled randomly on start.
   The global default period between two pings can overwritten with the `--period` option.

   The pod needs `NET_ADMIN` capabilities to be allowed to perform pings.

3. `discoverMDNS [--period <duration>]`

   Runs a mDNS service discovery. As a precondition the daemon set for the node network must be configured with `startMDNSServer: true`. In this case, a mDNS server is running on node port `5353` and is provided a service for its GRPC server. These services can be discovered with mDNS (UDP broadcast) if there are no network components like routers or firewalls between zones.

### Jobs as defined in the default configuration for the **node network**

#### Job ID `ping-n2n`

Ping from all pods of the daemon set of the node network to all known nodes.

#### Job ID `ping-n2api-ext`

Ping from all pods of the daemon set of the node network to the external address of the Kube API server.

#### Job ID `tcp-n2api-ext`

TCP connection check from all pods of the daemon set of the node network to the external address of the Kube API server.

#### Job ID `tcp-n2kubeproxy`

TCP connection check from all pods of the daemon set of the node network to the port 10249 used by the kubeproxy pods on all nodes (and running in the host network).

#### Job ID `mdns-n2n`

mDNS UDP broadcast discovery of the other nodes from all pods of the daemon set of the node.

#### Job ID `tcp-n2p`

TCP connection check from all pods of the daemon set of the node network to pod endpoints (pod IP, port of GRPC server) of the daemon set running in the pod network.

#### Job ID `nslookup-n`

Lookup of IP addresses for external DNS name `eu.gcr.io`, and external name of Kube API server.

### Jobs as defined in the default configuration for the **pod network**

#### Job ID `ping-p2n`

Ping from all pods of the daemon set on the pod network to all known nodes.

#### Job ID `ping-p2api-ext`

Ping from all pods of the daemon set on the pod network to the external address of the Kube API server.

#### Job ID `tcp-p2api-ext`

TCP connection check from all pods of the daemon set on the pod network to the external address of the Kube API server.

#### Job ID `tcp-p2api-int`

TCP connection check from all pods of the daemon set on the pod network to the internal address of the Kube API server (e.g. `100.64.0.1:443`).

#### Job ID `tcp-p2kubeproxy`

TCP connection check from all pods of the daemon set on the pod network to the port 10249 used by the kubeproxy pods on all nodes (and running in the host network).

#### Job ID `tcp-p2p`

TCP connection check from all pods of the daemon set on the pod network to pod endpoints (pod IP, port of GRPC server) of the daemon set running in the pod network.

#### Job ID `nslookup-p`

Lookup of IP addresses for external DNS name `eu.gcr.io`, and internal and external names of Kube API server.
