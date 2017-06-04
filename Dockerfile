FROM mesosphere/mesos-slave:1.2.0
MAINTAINER Joris Bonnefoy <joris.bonnefoy@corp.ovh.com>

RUN apt-get update && apt-get install -y make gcc

RUN curl -O https://www.kernel.org/pub/linux/utils/util-linux/v2.30/util-linux-2.30.tar.gz \
    && tar xvzf util-linux-2.30.tar.gz \
    && cd util-linux-2.30 \
    && ./configure \
    && make nsenter \
    && cp nsenter /usr/bin/

COPY ./go-mesos-executor /usr/libexec/mesos/mesos-docker-executor
