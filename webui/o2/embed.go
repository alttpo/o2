package main

//go:generate go-bindata -fs -nomemcopy -o bindata.go       -tags !debug        -ignore /\. -prefix ../dist ../dist/ ../dist/r/
//go:generate go-bindata -fs -nomemcopy -o bindata_debug.go -tags  debug -debug -ignore /\. -prefix ../dist ../dist/ ../dist/r/
