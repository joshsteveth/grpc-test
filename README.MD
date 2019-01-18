## Todo GRPC server 
####(initial idea doesn't belong to me btw)
current state is very messy, this repo only serves as my playground to test grpc behavior

#### proto definition
`todo/todo.proto`

#### Server
It runs 2 almost identical todo services on `8888` and `8889`. There difference is the used database `mydb.pb` and `mydb2.pb`, and if you call list on `8888` , it will also call list from `8889` and combine the result. 
Please build the binary and run it: 
`cd todo/cmd/server/ && go build`

#### Client
How to run:
`go run todo/cmd/todo/main.go {port} {cmd} {args...}`


For instance:
- `go run todo/cmd/todo/main.go 8888 list`
- `go run todo/cmd/todo/main.go 8889 add cooking potato`


#### Some details you might appreciate:
- I want to test how metadata between grpc services is passed inside context, so while port `8888` calls `8889`, you can see that only what insides of outgoing metadata will be passed into next service as incoming metadata.
- You can play around by modifying `NewRequestTracingUnaryInterceptor` , which comparable to handler's middleware
- or you can also modify `withClientTracingUnaryInterceptor`, which intercepts context while dialing another service. 
- metadata details can be observed by func `printAllMetadata`
- it's not very well written so use it at your own discretion. 