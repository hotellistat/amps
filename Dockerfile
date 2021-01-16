FROM scratch

COPY dist/bp /go/bin/bp

ENTRYPOINT ["/go/bin/bp"]