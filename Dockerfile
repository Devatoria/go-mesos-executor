FROM mesosphere/mesos-slave:1.2.0
MAINTAINER Joris Bonnefoy <joris.bonnefoy@corp.ovh.com>

COPY ./go-mesos-executor /usr/libexec/mesos/mesos-docker-executor
