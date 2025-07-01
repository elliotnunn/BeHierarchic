# BeHierarchic

**The Retrocomputing Archivist's File Server**

BeHierarchic exposes the inside of several Apple-adjacent archive types as a plain directory, like so:

```
Apple_Developer_Discs_1989-2009/
    1997/
        System Software/
            Dev.CD Nov 97 SSW.toast◆partitions/
                Apple_HFS,TOAST 2.5 Partition◆files/
                    Dev.CD Nov 97 SSW/
                        Utilities/
                            SimpleText◆resources/
                                CODE/
                                    9
```

## Get started

```
go install github.com/elliotnunn/BeHierarchic@latest
BeHierarchic :1997 ~/mysoftwarecollection
```

On a Mac: ⌘K and connect to http://127.0.0.1:1997
On Windows: navigate Windows Explorer to http://127.0.0.1:1997

Supported archive/image types include:

- Zip
- StuffIt
- HFS (Apple's old old Mac filesystem)
- resource forks
- more to come!
