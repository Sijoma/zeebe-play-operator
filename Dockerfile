FROM gcr.io/distroless/static:nonroot
COPY manager /manager
ENTRYPOINT ["/manager"]