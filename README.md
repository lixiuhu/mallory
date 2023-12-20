## mallory
HTTP/HTTPS proxy over SSH.

## Installation
* Local machine: `go install github.com/justmao945/mallory/cmd/mallory@latest`
* Remote server: need our old friend sshd

## Configuration
### Config file
Default path is `$HOME/.config/mallory.json`, can be set when start program
```
mallory -config path/to/config.json
```

Content:
* `id_rsa` is the path to our private key file, can be generated by `ssh-keygen`
* `local_smart` is the local address to serve HTTP proxy with smart detection of destination host
* `local_normal` is similar to `local_smart` but send all traffic through remote SSH server without destination host detection
* `remote` is the remote address of SSH server
* `blocked` is a list of domains that need use proxy, any other domains will connect to their server directly

```json
{
  "id_rsa": "$HOME/.ssh/id_rsa",
  "local_smart": ":1315",
  "local_normal": ":1316",
  "remote": "ssh://user@vm.me:22",
  "blocked": [
    "angularjs.org",
    "golang.org",
    "google.com",
    "google.co.jp",
    "googleapis.com",
    "googleusercontent.com",
    "google-analytics.com",
    "gstatic.com",
    "twitter.com",
    "youtube.com"
  ]
}
```

Blocked list in config file will be reloaded automatically when updated, and you can do it manually:
```
# send signal to reload
kill -USR2 <pid of mallory>

# or use reload command by sending http request
mallory -reload
```

### System config
* Set both HTTP and HTTPS proxy to `localhost` with port `1315` to use with block list
* Set env var `http_proxy` and `https_proxy` to `localhost:1316` for terminal usage

### Get the right suffix name for a domain
```
mallory -suffix www.google.com
```

### A simple command to forward all traffic for the given port
```sh
# install it: go get github.com/justmao945/mallory/cmd/forward

# all traffic through port 20022 will be forwarded to destination.com:22
forward -network tcp -listen :20022 -forward destination.com:22

# you can ssh to destination:22 through localhost:20022
ssh root@localhost -p 20022
```

### TODO
* return http error when unable to dial
* add host to list automatically when unable to dial
* support multiple remote servers

### Docker container

Considering the following config file:

```
$ cat mallory.json
{
  "id_rsa": "/tmp/id_rsa",
  "local_smart": ":1315",
  "local_normal": ":1316",
  "remote": "ssh://bhenrion@10.151.0.11:22"
}
```

You can run the container (`zoobab/mallory`) my mounting the config file, the SSH key, and mapping the 2 ports:

```
$ docker run -v $PWD/mallory.json:/root/.config/mallory.json -p 1316:1316 -p 1315:1315 -v $PWD/.ssh/id_rsa:/tmp/id_rsa zoobab/mallory
mallory: 2020/03/30 16:51:10 main.go:22: Starting...
mallory: 2020/03/30 16:51:10 main.go:23: PID: 1
mallory: 2020/03/30 16:51:10 config.go:103: Loading: /root/.config/mallory.json
mallory: 2020/03/30 16:51:10 main.go:30: Connecting remote SSH server: ssh://bhenrion@10.151.0.11:22
mallory: 2020/03/30 16:51:10 main.go:38: Local normal HTTP proxy: :1316
mallory: 2020/03/30 16:51:10 main.go:48: Local smart HTTP proxy: :1315
```

My use case was to connect to a Kubernetes cluster (Openshift) installed behind an SSH bastion:

```
$ export http_proxy=http://localhost:1316
$ export https_proxy=https://localhost:1316
$ oc login https://master.mycluster.zoobab.com:8443
Authentication required for https://master.mycluster.zoobab.com:8443 (openshift)
Username: bhenrion
Password:
Login successful.
```
