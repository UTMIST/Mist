# Permission System

Here is the design of the permission system to be implemented for use by the mist compute platform. The idea is very similar to unix groups.   

Each user will be tagged with workspaces (groups) they belong to and data storage sections that they may have read and or write access to. Beware that once this system is deployed it will need to be easy to migrate and switch between, so whatever implementation you make, make sure it easy to switch to and away from.

Demands the system needs to satisfy:

1. multiple people can be in multiple workspaces (for now workspace = group).
2. every job is attributed to some workspace or project, so it can be counted towards that groups resource use.
3. new workspaces need to be created easily.
4. system needs to be possible such as to allow future changes.
5. we can assume that users within a workspace trust each other and there are no significant malicious actors among the users.

Common groups formats (that will be inferfaced by the UI to appear that way, low level implementation shouldn't concern itself with this layer of abstraction) "personal," "team," "project," etc. 

Jobs will be tagged by the workspace they were launched from, and counted towards that workspace's resource use.

## Implementation

The system is most naturally represented as a bipartite graph of 3 sets: users linked to workspaces; and workspaces linked to data and jobs.

Data and job running permissions (mostly quotas, queues, priority) are attached to groups.

Present plans are to do everything using a very naive SQL database