FROM golang AS build

WORKDIR /app

COPY . .

RUN make build

RUN ls -la dist

FROM scratch

ARG version

ENV AMPS_VERSION=${version}

COPY --from=build /app/dist/amps /go/bin/amps

ENTRYPOINT ["/go/bin/amps"]