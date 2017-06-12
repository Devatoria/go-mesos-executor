.PHONY: all
all: build

.PHONY: build
build:
	go build

.PHONY: race
race:
	go build -race

.PHONY: test
test:
	go test ./... | grep -v /vendor/

.PHONY: testv
testv:
	go test -v ./... | grep -v /vendor/

.PHONY: docker
docker: build
	docker-compose build
	docker-compose up

.PHONY: clean
clean:
	docker-compose stop -t 0
	docker-compose rm -f
