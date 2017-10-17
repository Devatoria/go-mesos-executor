PROJECTNAME=mesos-executor
VERSION=$(shell git tag | sort -Vr | head -n 1)

.PHONY: all
all: build

.PHONY: deb
deb:	build
	mkdir -p ./$(PROJECTNAME)-deb/usr/sbin/
	mkdir -p ./$(PROJECTNAME)-deb/etc/mesos-executor/
	mkdir -p ./$(PROJECTNAME)-deb/DEBIAN
	chmod 0755 ./$(PROJECTNAME)-deb/DEBIAN
	cp ./config.yaml ./$(PROJECTNAME)-deb/etc/mesos-executor/
	cp ./go-mesos-executor ./$(PROJECTNAME)-deb/usr/sbin/mesos-container-executor
	printf "Package: mesos-executor\nMaintainer: Debian <docker@ml.ovh.net>\nVersion: $(VERSION)\nPriority: optional\nArchitecture: amd64\nSection: misc\nDepends: libc6\nDescription: Allow mesos to create docker containers\n" > $(PROJECTNAME)-deb/DEBIAN/control
	dpkg-deb --build $(PROJECTNAME)-deb .
	rm -rf ./$(PROJECTNAME)-deb

.PHONY: build
build:
	go build

.PHONY: race
race:
	go build -race

.PHONY: test
test:
	go test ./...

.PHONY: testv
testv:
	go test -v ./...

.PHONY: docker
docker: build
	docker-compose build
	docker-compose up

.PHONY: clean
clean:
	docker-compose stop -t 0
	docker-compose rm -f

.PHONY: redocker
redocker: clean docker

.PHONY: lint
lint:
	gometalinter ./... --vendor --exclude="^*_test.go" --disable=errcheck --cyclo-over=15
