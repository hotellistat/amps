FROM golang AS build

ARG version
ENV BATCHABLE_VERSION=$version

WORKDIR /app
COPY . .
RUN ls -la
RUN make build
RUN ls -la dist

FROM scratch
COPY --from=build /app/dist/batchable /go/bin/batchable
ENTRYPOINT ["/go/bin/batchable"]