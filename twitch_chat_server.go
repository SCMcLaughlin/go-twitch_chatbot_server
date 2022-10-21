package main

import (
    "fmt"
    "os"
    "time"
    "github.com/akamensky/argparse"
)

func main() {
    args := argparse.NewParser("twitch_chat_server", "description goes here")
    
    ircName := args.String("n", "name",
                          &argparse.Options{
                              Required: true,
                              Help: "the name of the Twitch account to connect with",
                          })
    ircOauthFilePath := args.String("o", "oauth",
                                    &argparse.Options{
                                        Required: true,
                                        Help: "path to a file containing the OAuth token to use for the Twitch account",
                                    })
    joinList := args.StringList("j", "join",
                                &argparse.Options{
                                    Required: false,
                                    Help: "specify a channel to join on startup, may be supplied multiple times",
                                })
    err := args.Parse(os.Args)
    if err != nil {
        fmt.Println(args.Usage(err))
        return
    }
    
    channelsToJoin := *joinList
    channelList := NewChannelList()
    
    for i := 0; i < len(channelsToJoin); i++ {
        channelList.Add(channelsToJoin[i])
    }
    
    server, err := NewServer()
    if err != nil {
        fmt.Fprintf(os.Stderr, "%s", err)
        return
    }
    
    for {
        irc, err := NewConnection(*ircName, 
                                  *ircOauthFilePath,
                                  channelList,
                                  server.CommandQueue,
                                  server.JsonQueue)
        if err != nil {
            fmt.Fprintf(os.Stderr, "%s", err)
            fmt.Println("Retrying in 5 seconds...")
            time.Sleep(5 * time.Second)
            continue
        }
        defer irc.Close()
        
        irc.JoinAllChannels()
        irc.MainLoop()
    }
}
