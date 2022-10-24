package main

type Command struct {
    Op string `json:"json_meta_type"`
    Channel string `json:"channel_name"`
    Message string `json:"msg"`
}
