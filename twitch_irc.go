package main

import (
    "fmt"
    "io/ioutil"
    "encoding/json"
    "net"
    "os"
    "regexp"
    "time"
)

const TWITCH_IRC_ADDR = "irc.chat.twitch.tv:6667"

type TwitchIrcConnection struct {
    conn net.Conn
    channelList *ChannelList
    mustReconnect chan(bool)
    outputLines chan(string)
    outputHandlers map[byte]func(*TwitchIrcConnection, string)
    cmdHandlers map[string]func(*TwitchIrcConnection, *Command)
    chatRegex *regexp.Regexp
    privmsgRegex *regexp.Regexp
    inputQueue chan(string)
    commandQueue chan(*Command)
    jsonQueue chan(string)
}

func NewConnection(name, oauthFilePath string, 
                   channelList *ChannelList,
                   commandQueue chan(*Command),
                   jsonQueue chan(string)) (*TwitchIrcConnection, error) {
    oauthToken, err := ioutil.ReadFile(oauthFilePath)
    if err != nil {
        fmt.Fprintf(os.Stderr, `Error: could not open OAuth file at "%s"`, oauthFilePath)
        return nil, err
    }
    
    conn, err := net.Dial("tcp", TWITCH_IRC_ADDR)
    if err != nil {
        return nil, err
    }
    
    _, err = conn.Write([]byte(fmt.Sprintf("PASS %s\r\nNICK %s\r\n", string(oauthToken[:]), name)))
    if err != nil {
        return nil, err
    }
    
    o := &TwitchIrcConnection{}
    o.conn = conn
    o.channelList = channelList
    o.mustReconnect = make(chan bool, 8)
    o.outputLines = make(chan string, 256)
    o.outputHandlers = map[byte]func(*TwitchIrcConnection, string) {
        'P': handlePing,
        'D': handleDisconnect,
        '@': handleChat,
    }
    o.cmdHandlers = map[string]func(*TwitchIrcConnection, *Command) {
        "join": cmdJoin,
        "leave": cmdLeave,
        "chat": cmdChat,
    }
    o.chatRegex = regexp.MustCompile(`tmi\.twitch\.tv (\w+) #([\w_]+)`)
    o.privmsgRegex = regexp.MustCompile(`@([\w_]+)\.tmi\.twitch\.tv PRIVMSG #[\w_]+ :(.+)`)
    o.inputQueue = make(chan string, 64)
    o.commandQueue = commandQueue
    o.jsonQueue = jsonQueue
    
    go receiveLinesFromTwitch(o)
    go sendLinesToTwitch(o)
    
    return o, nil
}

func receiveLinesFromTwitch(o *TwitchIrcConnection) {
    lineMatch := regexp.MustCompile(`([^\r\n]+)\r\n`)
    buf := make([]byte, 4096)
    rem := ""
    for {
        recvlen, err := o.conn.Read(buf)
        if err != nil {
            fmt.Fprintf(os.Stderr, "%s", err)
            o.mustReconnect <- true
            return
        }
        
        // find all the "\r\n"-terminated lines in the buffer;
        // if the buffer content doesn't end in "\r\n", remember
        // the content and prepend it onto the next line that does
        // end in "\r\n"
        indices := lineMatch.FindAllIndex(buf[:recvlen], -1)
        for i := 0; i < len(indices); i++ {
            var line string
            idx := indices[i]
            str := string(buf[idx[0]:idx[1]-2])
            
            if i == 0 {
                line = rem + str
                rem = ""
            } else {
                line = str
            }
            
            o.outputLines <- line
        }
        
        if len(indices) == 0 {
            rem += string(buf[:recvlen])
        } else {
            lastIndex := indices[len(indices)-1][1]
            if lastIndex < recvlen {
                rem += string(buf[lastIndex:recvlen])
            }
        }
    }
}

func (o *TwitchIrcConnection) write(str string) bool {
    _, err := o.conn.Write([]byte(str))
    if err != nil {
        o.mustReconnect <- true
        return false
    }
    return true
}

func sendLinesToTwitch(o *TwitchIrcConnection) {
    useQueue := false
    ticker := time.NewTicker(1500 * time.Millisecond)
    queue := make([]string, 0, 8)
    sendFrom := 0
    
    const MAX_QUEUE = 4096
    
    for {
        select {
        case str := <-o.inputQueue:
            if useQueue {
                if len(queue) < MAX_QUEUE {
                    queue = append(queue, str)
                }
            } else {
                // it has been more than 1500ms since the last send,
                // we can send immediately and restart the ticker for
                // any subsequent queued sends
                if !o.write(str) {
                    return
                }
                ticker.Reset(1500 * time.Millisecond)
                useQueue = true
            }
        case <-ticker.C:
            if sendFrom == len(queue) {
                if len(queue) > 0 {
                    // we've sent all the messages in the queue,
                    // shrink the queue to len = 0
                    queue = queue[:0]
                    sendFrom = 0
                    useQueue = false
                }
            } else {
                if !o.write(queue[sendFrom]) {
                    return
                }
                sendFrom++
            }
        }
    }
}

func (o *TwitchIrcConnection) MainLoop() {
    for {
        select {
        case line := <-o.outputLines:
            o.handleOutputLine(line)
        case cmd := <-o.commandQueue:
            o.handleCommand(cmd)
        case <-o.mustReconnect:
            fmt.Println("irc socket disconnected or errored")
            return
        }
    }
}

func (o *TwitchIrcConnection) handleOutputLine(line string) {
    c := line[0]
    handler, ok := o.outputHandlers[c]
    if ok {
        handler(o, line)
    }
}

func handlePing(o *TwitchIrcConnection, _ string) {
    o.inputQueue <- "PONG :tmi.twitch.tv\r\n"
}

func handleDisconnect(o *TwitchIrcConnection, _ string) {
    o.mustReconnect <- true
}

func handleChat(o *TwitchIrcConnection, line string) {
    match := o.chatRegex.FindStringSubmatch(line)
    if match == nil || len(match) < 3 {
        return
    }
    
    op := match[1]
    channelName := match[2]
    
    metadata := ChatMetadataToMap(line)
    metadata["json_meta_type"] = "irc"
    metadata["irc_channel"] = channelName
    metadata["irc_msg_type"] = op
    
    if op == "PRIVMSG" {
        match := o.privmsgRegex.FindStringSubmatch(line)
        
        if match != nil && len(match) == 3 {
            accountName := match[1]
            message := match[2]
            
            metadata["irc_account"] = accountName
            
            if message[0] != 0x01 {
                metadata["irc_msg"] = message
                //fmt.Printf("#%s %s: %s\n", channelName, metadata["display_name"], message)
            } else {
                // emotes start with a 0x01 byte, the word ACTION and a space, end with another 0x01
                header := "\x01ACTION "
                metadata["irc_emote"] = message[len(header):len(message)-2]
            }
        }
    }
    
    jsonStr, err := json.Marshal(metadata)
    if err != nil {
        return
    }
    
    o.jsonQueue <- string(jsonStr) + "\n"
}

func (o *TwitchIrcConnection) handleCommand(cmd *Command) {
    op := cmd.Op
    if len(op) > 0 {
        handler, ok := o.cmdHandlers[op]
        if ok {
            handler(o, cmd)
        }
    }
}

func cmdJoin(o *TwitchIrcConnection, cmd *Command) {
    channel := cmd.Channel
    if len(channel) > 0 {
        o.joinChannel(channel)
    }
}

func cmdLeave(o *TwitchIrcConnection, cmd *Command) {
    channel := cmd.Channel
    if len(channel) > 0 {
        o.leaveChannel(channel)
    }
}

func cmdChat(o *TwitchIrcConnection, cmd *Command) {
    ch := cmd.Channel
    msg := cmd.Message
    if len(ch) > 0 && len(msg) > 0 {
        o.chatInChannel(ch, msg)
    }
}

func (o *TwitchIrcConnection) joinChannelRaw(name string) {
    str := fmt.Sprintf("JOIN #%s\r\nCAP REQ :twitch.tv/tags\r\nCAP REQ :twitch.tv/commands\r\n",
                       name)
    o.inputQueue <- str
}

func (o *TwitchIrcConnection) joinChannel(name string) {
    if o.channelList.Add(name) {
        o.joinChannelRaw(name)
    }
}

func (o *TwitchIrcConnection) leaveChannel(name string) {
    if o.channelList.Remove(name) {
        str := fmt.Sprintf("PART #%s\r\n", name)
        o.inputQueue <- str
    }
}

func (o *TwitchIrcConnection) chatInChannel(channel, message string) {
    if o.channelList.IsInChannel(channel) {
        str := fmt.Sprintf("PRIVMSG #%s :%s\r\n", channel, message)
        o.inputQueue <- str
    }
}

func (o *TwitchIrcConnection) JoinAllChannels() {
    o.channelList.ForEach(func(channelName string) {
        o.joinChannelRaw(channelName)
    })
}

func (o *TwitchIrcConnection) Close() {
    o.conn.Close()
}
