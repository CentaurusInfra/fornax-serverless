.PHONY: test check build

check:
	@hostname
	@cat /etc/os-release
	@pwd
	@lscpu | grep CPU\(s\)
	@free -m
	make --version
	@echo "check is done"

build:
	@echo "to build all..."

test:
	@echo "to run all checkin tests..."
