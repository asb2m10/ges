# Examples

Throwaway scripts used for manually exercising `ges` (see `docs/spec.md` §5.1
for entry configuration blocks).

- `testjob.sh` — minimal job: prints a line and its `GES_SPOOL_DIR`.
- `dd/ddjob.sh` — demonstrates the `dd` directive: links the companion file
  `dd/myfile_example` into the job via `DD_MYFILE_EXAMPLE`.

Try it:

```sh
go build -o ges .
./ges submit ./examples/testjob.sh
./ges submit ./examples/dd/ddjob.sh
./ges jobs
./ges job <job-number>
```
