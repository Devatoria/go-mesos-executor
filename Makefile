.PHONY: all
all: build

.PHONY: build
build:
	go build

.PHONY: test
test:
	go test

.PHONY: docker
docker: build
	docker-compose up

.PHONY: clean
clean:
	docker-compose stop
	docker-compose rm -f
