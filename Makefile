.PHONY: all
all: build

.PHONY: build
build:
	go build

.PHONY: test
test:
	go test -v ./...

.PHONY: docker
docker: build
	docker-compose up

.PHONY: clean
clean:
	docker-compose stop -t 0
	docker-compose rm -f
