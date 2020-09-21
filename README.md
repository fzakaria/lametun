# lametun

This is a very minimalistic demonstration of how to setup a VPN-_like_ tunnel.
It offers **no encryption** and is meant solely as a learning excercise.

One of the machines running _lametun_ must have a public IP & will be the designated "server".
```bash
./lametun -listen
```

The other machine which may behind a NAT must specify the public IP of the server.
```bash
./lametun -server 54.219.126.112
```

The _default port_ is **1234**. Make sure to allow the UDP port in your firewall rules or cloud VPCs.