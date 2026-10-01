# A datasource change takes effect without a restart

An administrator enables, disables, configures and orders datasources from the admin
area. The registry is rebuilt from the table by the RPC that changed a row, so the change
takes effect at once in the process that served it, and a resolution reads the enabled
entries once as it starts so one run sees one precedence throughout. A datasource's rate
limiter outlives a reload, so a change does not reset the quota spent.

The RPC refuses to enable a datasource without an integration in the build, where the
same row at startup stops the process: startup has nothing better to do, and the RPC has
an administrator to tell.

One process holds one registry. Reaching other processes is not addressed, since the
service runs as one.
