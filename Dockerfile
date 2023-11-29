FROM ubuntu
ENTRYPOINT ["/usr/bin/runtime-extensions-poc"]
EXPOSE 9443

COPY ./runtime-extensions-poc /usr/bin/runtime-extensions-poc
