### Juggler :juggling_person:


Juggler is a small CLI tool I built to execute a series of `virsh attach-device` and `virsh detach-device` commands I was doing repeatedly.

It will also help you figure out which VMs have the devices you're looking for, and will make sure to attach them in alphabetical order.

TODO: allow user to configure the socket

### Usage

Just `go build` and you should get


```
NAME:
   juggler - Juggle your PCI devices among libvirt VMs! 
             juggler will attach, detach a directory PCI devices to/from a domain in alphabetical order

USAGE:
   juggler [global options] command [command options] [arguments...]

COMMANDS:
   attach   Attach a set of devices to a domain
   detach   Detach devices from all domains
   find     Find which domain has the given devices
   help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --help, -h  show help
```

