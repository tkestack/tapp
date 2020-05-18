.PHONY: all
all: verify-gofmt build test

.PHONY: clean
clean:
	rm -rf bin/ _output/ go .version-defs

.PHONY: build
build:
	hack/build.sh

# Run test
#
# Args:
# 		TFLAGS: test flags
# Example:
# 		make test TFLAGS='-check.f xxx'
.PHONY: test
test:
	TESTFLAGS=$(TFLAGS) hack/test-go.sh

.PHONY: verify
verify:
	hack/verify-all.sh

.PHONY: verify-gofmt
verify-gofmt:
	hack/verify-gofmt.sh

format:
	hack/format.sh

build-image: verify-gofmt
	hack/build-image.sh tkestack/tapp-controller:latest

push-image:
	hack/push-image.sh tkestack/tapp-controller:latest

release: build-image push-image

#  vim: set ts=2 sw=2 tw=0 noet :
