FROM golang:1.19 as build
WORKDIR $GOPATH/mocklet
COPY ./ ./
RUN GOOS=linux GOARCH=amd64 go build -o /mocklet


FROM ubuntu

RUN mkdir k8s

COPY --from=build mocklet k8s/

COPY config.yaml k8s/

COPY private.key k8s/

COPY ca.crt k8s/

WORKDIR k8s/

ENV OCAGENT_INSECURE=yes
ENV APISERVER_CERT_LOCATION=ca.crt
ENV APISERVER_KEY_LOCATION=private.key

CMD ["./mocklet","--provider-config=/k8s/config.yaml","--nodename=mocklet","--enable-node-lease=true"]