BINDIR=bin
BIN=run-command-handler
IMMEDIATE_BIN=immediate-run-command-handler
BIN_ARM64=run-command-handler-arm64
IMMEDIATE_BIN_ARM64=immediate-run-command-handler-arm64
BUNDLEDIR=bundle
BUNDLE=run-command-handler.zip
LDFLAGS_COMMON=-extldflags '-static' -m 1 -o  '<Version>(.*)</Version>' misc/manifest.xml | awk -F">" '{print $$2}' | awk -F"<" '{print $$1}'`
LDFLAGS_MAIN=-X main.Version=`grep ${LDFLAGS_COMMON}
LDFLAGS_IMMEDIATE=-X immediateruncommandservice.Version=`grep ${LDFLAGS_COMMON}

bundle: clean binary
	$(info creating $(BUNDLEDIR) directory)
	@mkdir -p $(BUNDLEDIR)

	$(info creating zip $(BUNDLEDIR)/$(BUNDLE) with contents from $(BINDIR) directory)
	zip -r ./$(BUNDLEDIR)/$(BUNDLE) ./$(BINDIR)
	zip -j ./$(BUNDLEDIR)/$(BUNDLE) ./misc/HandlerManifest.json
	zip -j ./$(BUNDLEDIR)/$(BUNDLE) ./misc/manifest.xml

binary: clean
	$(info building amd64 binaries)
	GOOS=linux GOARCH=amd64 go build -v \
	  -ldflags "${LDFLAGS_MAIN}" -o $(BINDIR)/$(BIN) ./cmd/main 

	$(info building amd64 immediate run command service)
	OOS=linux GOARCH=amd64 go build -v \
	  -ldflags "${LDFLAGS_IMMEDIATE}" -o $(BINDIR)/$(IMMEDIATE_BIN) ./cmd/immediateruncommandservice

	$(info building arm64 binaries)
	GOOS=linux GOARCH=arm64 go build -v \
	  -ldflags "${LDFLAGS_MAIN}" -o $(BINDIR)/$(BIN_ARM64) ./cmd/main
	
	$(info building amd64 immediate run command service)
	GOOS=linux GOARCH=arm64 go build -v \
	  -ldflags "${LDFLAGS_IMMEDIATE}" -o $(BINDIR)/$(IMMEDIATE_BIN_ARM64) ./cmd/immediateruncommandservice

	$(info copy run-command-shim into $(BINDIR))
	cp ./misc/run-command-shim ./$(BINDIR)

clean:
	$(info cleaning $(BINDIR) and $(BUNDLEDIR) directories)
	rm -rf "$(BINDIR)" "$(BUNDLEDIR)"
	$(info directories cleaned)

.PHONY: clean binary
