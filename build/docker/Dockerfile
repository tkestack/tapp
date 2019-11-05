From alpine:3.9

ADD tapp-controller /usr/local/bin
RUN mkdir /etc/certs
ADD ca.crt /etc/certs
ADD tls.crt /etc/certs
ADD tls.key /etc/certs

ENTRYPOINT ["/usr/local/bin/tapp-controller"]
