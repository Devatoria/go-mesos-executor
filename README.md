# go-mesos-executor
Custom Apache Mesos executor written in Go

## TODO

* Send TASK_FAILED to agent when throwing an error while running (eg. if we fail to run a container) (actually panic)
* Find a way to send errors from executor to scheduler (eg. display custom message in Marathon UI debug tab)
* Implement hooks manager
* Implement docker parameters
** Network mode
** Privileged
** Memory/CPU limit
** Mounts
