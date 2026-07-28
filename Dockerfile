# Build on the native runner arch and cross-compile — Go needs no emulation.
FROM --platform=$BUILDPLATFORM golang AS build

WORKDIR /app

COPY . .

ARG TARGETARCH

RUN GOARCH=${TARGETARCH:-amd64} make build

RUN ls -la dist

FROM scratch

ARG version

ENV AMPS_VERSION=${version}

COPY --from=build /app/dist/amps /go/bin/amps

ENTRYPOINT ["/go/bin/amps"]