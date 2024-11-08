module github.com/willgorman/libiscsi-go

go 1.23.1

toolchain go1.23.3

require (
	github.com/avast/retry-go/v4 v4.5.1
	github.com/gostor/gotgt v0.2.2
	github.com/hashicorp/consul/sdk v0.16.1
	github.com/mattn/go-pointer v0.0.1
	github.com/sanity-io/litter v1.5.5
	golang.org/x/sys v0.26.0
)

require (
	github.com/augustoroman/hexdump v0.0.0-20231204223853-3694912baadb // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/satori/go.uuid v1.2.0 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/stretchr/testify v1.9.0 // indirect
	gotest.tools v2.2.0+incompatible // indirect
)

replace github.com/gostor/gotgt => /home/will/src/gotgt-1
