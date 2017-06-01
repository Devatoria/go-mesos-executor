# go-mesos-executor

This is a custom container Apache Mesos executor written in Go. It actually can launch Docker containers but is designed to accept others container engines as soon as they implement the container interface. The particuliarity of this executor is the fact that it can use hooks to do actions in pre-start, post-start and post-start.

## Hooks system

This executor implements a hook system, allowing you to run custom functions on `pre-create`, `pre-run`, `post-run`, `pre-stop` and `post-stop`. The `containerizer` and the task information are passed to hooks so you can interact with containers directly or even manipulate task information **before running the container**.

You can take a look into the `hook` folder to view some examples.

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
  * Volumes
  * Command health checks

The executor actually does not handle custom parameters sent to Docker CLI. This has to be done with a matching enum (I think) and it is actually a little bit boring to do this :)
