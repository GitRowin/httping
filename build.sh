#!/bin/bash
goos=windows goarch=amd64 go build -ldflags="-s -w" -trimpath -o httping-windows-amd64
goos=linux goarch=amd64 go build -ldflags="-s -w" -trimpath -o httping-linux-amd64
goos=linux goarch=arm64 go build -ldflags="-s -w" -trimpath -o httping-linux-arm64
