FROM mesosphere/mesos-slave:1.3.1
MAINTAINER Joris Bonnefoy <joris.bonnefoy@corp.ovh.com>

COPY ./go-mesos-executor /usr/libexec/mesos/mesos-docker-executor
