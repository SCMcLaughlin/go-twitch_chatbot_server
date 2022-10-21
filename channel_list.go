package main

type ChannelList struct {
    array []string
    set map[string]struct{}
}

type void struct{}
var set_member void

func NewChannelList() *ChannelList {
    return &ChannelList{
        array: make([]string, 0),
        set: make(map[string]struct{}),
    }
}

func (o *ChannelList) Add(channelName string) bool {
    _, ok := o.set[channelName]
    if !ok {
        o.set[channelName] = set_member
        o.array = append(o.array, channelName)
        return true
    }
    return false
}

func (o *ChannelList) Remove(channelName string) bool {
    _, ok := o.set[channelName]
    if ok {
        delete(o.set, channelName)
        
        // find index in array
        var idx int
        for i := 0; i < len(o.array); i++ {
            if o.array[i] == channelName {
                idx = i
                break
            }
        }
        // swap and pop
        o.array[idx] = o.array[len(o.array)-1]
        o.array = o.array[:len(o.array)-1]
        
        return true
    }
    return false
}

func (o *ChannelList) IsInChannel(channelName string) bool {
    _, ok := o.set[channelName]
    return ok
}

func (o *ChannelList) ForEach(fn func(channelName string)) {
    for i := 0; i < len(o.array); i++ {
        fn(o.array[i])
    }
}
