module controller-daemon

go 1.22.2

require webcontroller/config v0.0.0

replace webcontroller/config => ../config

require go.bug.st/serial v1.6.4

require (
	github.com/creack/goselect v0.1.2 // indirect
	golang.org/x/sys v0.19.0 // indirect
)
