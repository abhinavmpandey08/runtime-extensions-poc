.PHONY: build docker-build
docker-build: build
	docker build -t runtime-extensions-poc .

build: 
	go build .