FROM golang AS build
WORKDIR /app
COPY . .
RUN ls -la
RUN make build
RUN ls -la dist

FROM scratch
ARG version
ENV BATCHABLE_VERSION=${version}
COPY --from=build /app/dist/batchable /go/bin/batchable
ENTRYPOINT ["/go/bin/batchable"]