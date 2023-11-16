## Ideas

* Completely fill a volume with random data
* write marker values to each lba of a volume
* io.Reader/io.Writer backed by iscsi
  * how does the block limit affect this?  write would have to write a partially empty block and be able to keep track of where to resume writing.

## TODO

* build a binary cross-platform for linux
