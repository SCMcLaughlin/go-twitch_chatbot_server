package main

import (
    "strings"
)

func ChatMetadataToMap(str string) map[string]string {
    metadata := make(map[string]string, 64)
    
    // skip the first char, we already know it is '@'
    start := 1
    i := 1
    n := len(str)
    equals := 0
    
    for i < n {
        c := str[i]
        
        // allow escaping of control characters
        if c == '\\' {
            i += 2
            continue
        }
        
        // end of metadata is signalled by an ascii space
        if c == ' ' {
            break
        }
        
        // "key=value;" pairs
        if c == ';' {
            if equals != 0 {
                key := str[start:equals]
                val := str[equals+1:i]
                
                key = strings.Replace(key, "-", "_", -1)
                metadata[key] = val
            }
            
            start = i + 1
        } else if c == '=' {
            equals = i
        }
        
        i++
    }
    
    return metadata
}
