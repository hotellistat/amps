FROM alpine AS build
RUN apk update
RUN apk upgrade
RUN apk add --update go gcc g++ make
WORKDIR /app
COPY . .
RUN make build

FROM alpine
WORKDIR /app
RUN cd /app
COPY --from=build /app/dist .
ENTRYPOINT ["/app/batchable"]