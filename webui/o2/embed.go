package main

//go:generate go-bindata -fs -nomemcopy -o bindata.go -tags !debug -prefix ../static ../static
//go:generate go-bindata -fs -nomemcopy -o bindata_debug.go -tags debug -debug -prefix ../static ../static
