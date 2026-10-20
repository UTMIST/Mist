This is a project designing the compute platform for UTMIST (University of Toronto Machine Intelligence Student Team). The target cluster is two computers, one with tenstorrent gpu and one with nvidia gpus.

## Documentation

Agents are allowed to edit the README and top level markdown files as well as docs/agents, but shouldn't touch docs/humans as those are by humans for humans (though agents may notify humans if they believe human docs are severely outdated).

## Temporary Decisions

Make the frontend lightweight and move logic to backend. We aren't short on bandwidth, so it is fine to send a new qpi request and refresh UI every once in a while.