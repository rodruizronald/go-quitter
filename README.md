# Graceful shutdown with Go Quitter

Golang is a langue known for its simplicity, performance, and concurrency model. Goroutines are one of the most basic units to organize a Go program. A goroutine is a lightweight execution thread that executes concurrently, but not necessarily in parallel, with the rest of the program.

Working with multi-threaded applications can be challenging, especially when it comes to enabling a graceful shutdown mechanism. The goal of a graceful shutdown is to ensure that background threads are not abruptly terminated in the middle of a critical operation, which could lead to data corruption or other issues.

For example, let's consider a Go program that forks 20 goroutines from the main routine, and each of them creates another 20 goroutines, and so on. With so many goroutines running in the background, it becomes crucial to implement a reliable mechanism for shutting down the program gracefully. When the main routine receives a termination signal, it needs to inform all the goroutines running in the background to exit gracefully. This can be achieved using channels and signals, which are core features of the Go programming language.

This repository introduces the go-quitter, a tool designed to send a quit signal to goroutines, allowing them to perform necessary cleanup tasks and save their states. When using go-quitter, the program will wait for a set amount of time before exiting, ensuring that all necessary tasks are completed while avoiding indefinite locks. Additionally, go-quitter can identify routines that fail to return. This feature is particularly useful for debugging and identifying potential issues in the code.

Overall, go-quitter is a useful solution for managing background processes in Go programs and ensuring they gracefully exit when necessary.

To get started, look at the following examples:

- [HTTP server](example/http_server/http_server.go)
- [Heartbeat](example/heartbeat/heartbeat.go)
