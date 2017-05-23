# go-mesos-executor

This is a custom container Apache Mesos executor written in Go. It actually can launch Docker containers but is designed to accept others container engines as soon as they implement the container interface. The particuliarity of this executor is the fact that it can use hooks to do actions in pre-start, post-start and post-start.

## Test it

If you want to test the executor, you can run `make docker`. It will create a basic stack with:
* ZooKeeper
* Mesos master
* Mesos agent
* Marathon

You can access Marathon framework UI using your `http://<IP>:8080/`. Executor logs are in the `stderr` of the task sandbox. When finished, please run `make clean` to remove stack containers.

## TODO

* Send TASK_FAILED to agent when throwing an error while running (eg. if we fail to run a container) (actually panic)
* Find a way to send errors from executor to scheduler (eg. display custom message in Marathon UI debug tab)
* Implement hooks manager
* Implement docker parameters
** Commands
** Volumes
** Command health checks
* Tests
* Remove logrus to use zap

The executor actually does not handle custom parameters sent to Docker CLI. This has to be done with a matching enum (I think) and it is actually a little bit boring to do this :)
