module controller-daemon

go 1.27.3

replace webcontroller/config => ../config

require (
	go.bug.st/serial v1.6.4
	webcontroller/config v0.0.0
)

require (
	github.com/creack/goselect v0.1.2 // indirect
	golang.org/x/sys v0.44.0 // indirect
)
