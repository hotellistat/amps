FROM alpine AS build
RUN apk add --no-cache go git make build-base
WORKDIR /app
COPY . .
RUN make build

FROM alpine
WORKDIR /app
RUN cd /app
COPY --from=build /app/dist .
ENTRYPOINT ["/app/batchable"]