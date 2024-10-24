mctsd - mail classifier training submission daemon

An http sserver expoeing an endpoint for submission of rspamd training samples using the rspamc client

POST /learn/{class}/{username}
send a filename with the identifier 'file'

Example client:
```
curl -F "file=$email_content_file" http://localhost:2015/learn/spam/username
```
