pushd webui/web && npm run production && popd
CGO_ENABLED=1 goreleaser --snapshot --clean
