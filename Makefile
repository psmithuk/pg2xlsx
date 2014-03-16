build:
	#
	# Build
	#

	go get github.com/lib/pq
	go get github.com/psmithuk/xlsx
	
	go build -o pg2xlsx main.go

dist: build
	
	$(eval VER := $(shell ./pg2xlsx -version))
	$(eval DISTPATH := dist/$(VER))

	gox -osarch="darwin/amd64 linux/amd64 windows/amd64"

	#
	# Creating Archive for $(VER)
	#
	
	mkdir -p $(DISTPATH)
	rm -rf $(DISTPATH)/*

	# for now, consider `allow.txt' config file to be part of application under source control
	cp pg2xlsx_darwin_amd64 pg2xlsx_linux_amd64 pg2xlsx_windows_amd64.exe $(DISTPATH)/;

.PHONY: build