package main

//go:generate go-bindata -fs -nomemcopy -o bindata.go       -tags !debug        -ignore /\. -prefix ../content/webroot ../content/webroot/ ../content/webroot/r/
//go:generate go-bindata -fs -nomemcopy -o bindata_debug.go -tags  debug -debug -ignore /\. -prefix ../content/webroot ../content/webroot/ ../content/webroot/r/
