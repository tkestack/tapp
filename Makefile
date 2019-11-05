.PHONY: all
all: verify-gofmt
	hack/build.sh

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

image: verify-gofmt
	hack/build-image.sh

#  vim: set ts=2 sw=2 tw=0 noet :
