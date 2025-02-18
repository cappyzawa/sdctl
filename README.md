## sd-cli

```
validate yamls, handle banners, start build from CLI

Usage:
  sdctl [flags]
  sdctl [command]

Available Commands:
  banner            handle screwdriver banners
  build             start a job.
  clear             clear your setting and set to default
  context           handle screwdriver contexts
  get               get sdctl settings and Screwdriver.cd information
  help              Help about any command
  set               set sdctl settings
  validate          validate your screwdriver.yaml, default to screwdriver.yaml
  validate-template validate your sd-template.yaml, default to sd-template.yaml

Flags:
  -h, --help      help for sdctl
      --version   version for sdctl

Use "sdctl [command] --help" for more information about a command.```

```

### Setup
In case of using your screwdriver.cd cluster
- Install sdctl

```
$ go get github.com/tk3fftk/sdctl
```
- Get screwdriver user token from https://<your_screwdrivercd>/user-settings
- Set configurations
```
$ sdctl set token <obtained-token>
$ sdctl set api https://<your_screwdrivercd>
```

### Usage
- start build
```
$ sdctl build <pipelineid> <start_from>
```

- validate screwdriver.yaml
```
$ sdctl validate
or
$ sdctl v
```

- validate sd-template.yaml
```
$ sdctl validate-tempalte
or
$ sdctl vt
```

- get build pages from build id
```
$ sdctl set jwt
$ sdctl get build-pages "156442 156518 323281"
```

- switch another screwdriver.cd
```
$ sdctl context set next
$ sdctl set token <obtained-token>
$ sdctl set api https://<your_screwdrivercd>
```

- get banners
```
$ sdctl banner get
ID	IsActive	Message
22	false	testtesttest
```

- create a banner
```
$ sdctl banner set -m "test message"
Successfully POST a banner ID 28
$ sdctl banner get
ID	IsActive	Message
28	true	test message
```

- update a banner
```
$ sdctl banner set -i 28 -m "UPDATED: test message"
Successfully PUT a banner ID 28
$ sdctl banner get
ID	IsActive	Message
28	true	UPDATED: test message
```

- delete a banner
```
$ sdctl banner set -i 28 -d
Successfully DELETE a banner ID 28
$ sdctl banner get
ID	IsActive	Message
```

- write a secret
```bash
$ sdctl secret set -p 1111 -k FOO -v bar 
setting secret FOO is succuseed!
```
