# Graceful shutdown with Go Quitter

Golang is a langue known for its simplicity, performance, and concurrency model. Goroutines are one of the most basic units to organize a Go program. A goroutine is a lightweight execution thread that executes concurrently, but not necessarily in parallel, with the rest of the program. Every program has at least one goroutine, the main goroutine, from which new goroutines can be forked depending on the application's needs.

One of the main issues when working with multi-threaded applications is enabling a graceful shutdown so that threads running in the background are not abruptly terminated in the middle of a critical operation if any. Let's imagine a Go program forks 20 goroutines from the main routine, and then from each of them another 20 and keep going if you wish. The idea is that many routines are running in the background. Now, imagine the main routine receives a termination signal. How would you inform all those routines in the background that it's time to exit? How much time do you wait before exiting? 

The go quitter aims to provide a graceful shutdown mechanism that propagates a quit signal to all goroutines running in the background and waits up to a predefined amount of time before exiting to allow the processes running in the background to clean and save their running states if needed.

To get started, look at the following examples:

- [HTTP server](example/http_server/http_server.go)
- [Heartbeat](example/heartbeat/heartbeat.go)

<p align="center">
  <img src="img/golang.png"/>
</p>
