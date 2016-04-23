#Tunnel-Digger
a simple tool let you and your friends connect your server from anyway.

##Features/Limitations

- raw tcp connection *only* now - you should do some kind of encryption yourself !
- each user's has server up to 255 TCP channels

##Architecture

- A : your computer (can't connect from outside)
- B : Tunnel-Digger's server
- C : user/client

About the line:
- `===>` : up to 255 connection, right side do listen
- `--->` : 1 connection only, right side do listen

```
 |              A               |                  |     B      |      |    C   |
 | [local server]  <===  [jump] | ---------------> |   server   | <=== | client |
 |                              |    {network}     |            |      |        |
```


##TODO
- [ ] add support for more protocol
- [ ] auto re-connect
- [ ] add TLS/SSL or some else encryption

