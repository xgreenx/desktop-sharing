#!/usr/bin/env bash
wget https://dl.google.com/go/go1.12.6.linux-amd64.tar.gz

sudo tar -xvf go1.12.6.linux-amd64.tar.gz
sudo mv go /usr/local

sudo rm -R go1.12.6.linux-amd64.tar.gz