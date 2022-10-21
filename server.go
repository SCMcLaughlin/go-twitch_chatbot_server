package main

import (
    "encoding/json"
    "net"
    "strings"
)

const SERVER_LISTENER_ADDR = ":44632"

type Server struct {
    CommandQueue chan(*Command)
    JsonQueue chan(string)
    newEndpoint chan(net.Conn)
    nextId uint64
    endpoints []*endpoint
    deleteEndpoint chan(uint64)
}

type endpoint struct {
    id uint64
    conn net.Conn
    queue chan(string)
    stop chan(bool)
}

func NewServer() (*Server, error) {
    o := &Server{
        CommandQueue: make(chan *Command, 32),
        JsonQueue: make(chan string, 64),
        newEndpoint: make(chan net.Conn, 32),
        nextId: 1,
        endpoints: make([]*endpoint, 0, 16),
        deleteEndpoint: make(chan uint64, 32),
    }
    
    listener, err := net.Listen("tcp", SERVER_LISTENER_ADDR)
    if err != nil {
        return nil, err
    }
    
    go listenForConnections(o, listener)
    go handleJsonQueue(o)
    
    return o, nil
}

func handleJsonQueue(server *Server) {
    for {
        select {
        case str := <-server.JsonQueue:
            for i := 0; i < len(server.endpoints); i++ {
                server.endpoints[i].queue <- str
            }
        case conn := <-server.newEndpoint:
            ep := &endpoint{
                id: server.nextId,
                conn: conn,
                queue: make(chan string, 256),
                stop: make(chan bool, 1),
            }
            server.nextId++
            server.endpoints = append(server.endpoints, ep)
            
            go handleEndpointOutput(ep, server.deleteEndpoint)
            go handleEndpointInput(ep, server.deleteEndpoint, server.CommandQueue)
        case id := <-server.deleteEndpoint:
            // find index for the endpoint with the given id
            var idx int
            for i := 0; i < len(server.endpoints); i++ {
                if server.endpoints[i].id == id {
                    idx = i
                    server.endpoints[i].stop <- true
                    server.endpoints[i].conn.Close()
                    break
                }
            }
            // swap and pop
            server.endpoints[idx] = server.endpoints[len(server.endpoints)-1]
            server.endpoints = server.endpoints[:len(server.endpoints)-1]
        }
    }
}

func listenForConnections(server *Server, listener net.Listener) {
    for {
        conn, err := listener.Accept()
        if err != nil {
            return
        }
        server.newEndpoint <- conn
    }
}

func handleEndpointOutput(ep *endpoint, deleteEndpoint chan(uint64)) {
    for {
        select {
        case str := <-ep.queue:
            _, err := ep.conn.Write([]byte(str))
            if err != nil {
                deleteEndpoint <- ep.id
                return
            }
        case <-ep.stop:
            deleteEndpoint <- ep.id
            return
        }
    }
}

func handleEndpointInput(ep *endpoint, deleteEndpoint chan(uint64), cmdQueue chan(*Command)) {
    buf := make([]byte, 4096)
    rem := ""
    for {
        recvlen, err := ep.conn.Read(buf)
        if err != nil {
            deleteEndpoint <- ep.id
            return
        }
        
        str := rem + string(buf[:recvlen])
        for {
            idx := strings.Index(str, "\n")
            if idx == -1 {
                rem = str
                break
            }
        
            // expecting input to be one-line json strings describing commands
            cmd := &Command{}
            err = json.Unmarshal([]byte(str[:idx]), cmd)
            
            if idx < (len(str)-1) {
                str = str[idx+1:]
            }
            
            if err != nil {
                continue
            }
            
            cmdQueue <- cmd
        }
    }
}
