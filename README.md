# go-mesos-executor

This is a custom container Apache Mesos executor written in Go. It actually can launch Docker containers but is designed to accept others container engines as soon as they implement the container interface. The particuliarity of this executor is the fact that it can use hooks to do actions in pre-start, post-start and post-start.

## Hooks system

This executor implements a hook system, allowing you to run custom functions on `pre-create`, `pre-run`, `post-run`, `pre-stop` and `post-stop`. The `containerizer` and the task information are passed to hooks so you can interact with containers directly or even manipulate task information **before running the container**.

You can take a look into the `hook` folder to view some examples.

### Remove containers hook

This hook removes the stopped container after shutting down (or task kill) in order to keep containerizer clean.

### ACL hook

This hook injects iptables rules in the container network namespace. To use it, just pass a list of IP, comma separated, with or without CIDR, to the `EXECUTOR_ALLOWED_IP` label. For example: `EXECUTOR_ALLOWED_IP: 8.8.8.8,10.0.0.0/24`.

When at least one IP is injected, the hook appends two extra rules at the very end:

* one to allow health checks (would be removed when executor HTTP(S)/TCP health checks will be supported) from container gateway
* one to drop all the traffic (maybe we should just change the default policy instead of adding an extra rule for this)

## Health checks

Since Mesos 1.1.0, health checks should be done locally, by the executor, in order to avoid network issues and be more scalable. Because it would not be a good idea to make these checks to be containerizer dependent (hammering the Dockder daemon for this is not a good idea), these checks should be executor directly in the container namespace (network for HTTP(S)/TCP, mount for commands). This can be done easily (and is already done in the ACL hook).

Mesos native health checks are actually not available in Marathon UI, and can be tested only when sending raw JSON. You must prefix the check protocol with `MESOS_`.

### Design

Health check design is to define, but will be simple: a health checker, which is a simple channel based worker with a slice of checks to executor periodically. The executor struct must have its own health checker which sends results associated to task ID, and then update status to healthy or unhealthy if needed.

### References

* http://mesos.apache.org/documentation/latest/health-checks/#mesos-native-health-checks
* http://mesos.apache.org/documentation/latest/health-checks/#under-the-hood
* https://github.com/mesosphere/health-checks-scale-tests/blob/06ac7a2f0fa52836fedfd4b26fdb5420b5ab207e/README.md

## Configuration file

Configuration file allows you to enable or not some hooks. The file must be named `config.yaml` and placed at the same place than the binary, or in `/etc/mesos-executor`. There's an example at project root.

## Test it

If you want to test the executor, you can run `make docker`. It will create a basic stack with:
* ZooKeeper
* Mesos master
* Mesos agent
* Marathon

You can access Marathon framework UI using your `http://<IP>:8080/`. Executor logs are in the `stderr` of the task sandbox. When finished, please run `make clean` to remove stack containers.

## Run tests

You can run unit tests using `make test`.

## TODO

* Send TASK_FAILED to agent when throwing an error while running (eg. if we fail to run a container) (actually panic)
* Find a way to send errors from executor to scheduler (eg. display custom message in Marathon UI debug tab)
* Implement docker parameters
  * Commands
* Add thread lock in ACL tests to avoir namespace switching
* Find a way to handle health checks with ACL hook when masquerading is enabled (leading to see the caller IP in the container instead of the bridge IP)
* Manage hooks priority
* Add some useful hooks
  * Error on privileged containers
  * Volumes sandboxing
  * Forced network mode (bridged)
* Health checks: please take a look at health checks section
  * HTTP(S)
  * TCP
  * Commands

The executor actually does not handle custom parameters sent to Docker CLI. This has to be done with a matching enum (I think) and it is actually a little bit boring to do this :)
