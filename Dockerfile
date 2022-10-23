FROM gcr.io/distroless/static:nonroot
COPY zeebe-play-operator /zeebe-play-operator
ENTRYPOINT ["/zeebe-play-operator"]