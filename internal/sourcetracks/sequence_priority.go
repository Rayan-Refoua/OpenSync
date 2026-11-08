package sourcetracks

import (
 "fmt"
 "path"
 "sort"
 "strconv"
 "strings"

 "opensync/internal/projectstate"
)

// SequenceNeighbor is a filename-derived acquisition-order hypothesis.
// Adjacent numbers do not imply continuity, overlap, or synchronization.
type SequenceNeighbor struct {
 Before string `json:"before"`
 After string `json:"after"`
 Directory string `json:"directory"`
 Series string `json:"series"`
 Basis string `json:"basis"`
 Continuity string `json:"continuity"`
}

type numberedRecording struct {
 ID string
 Directory string
 Series string
 Number uint64
}

// numberedSource recognizes a terminal decimal counter after an otherwise
// identical filename prefix. It deliberately does not combine cameras with
// different prefixes or directories. The counter is not a timecode.
func numberedSource(id, source string) (numberedRecording, bool) {
 normalized := strings.ReplaceAll(source, "\\", "/")
 base := path.Base(normalized)
 dot := strings.LastIndexByte(base, '.')
 if dot <= 0 { return numberedRecording{}, false }
 stem := base[:dot]
 i := len(stem)
 for i > 0 && stem[i-1] >= '0' && stem[i-1] <= '9' { i-- }
 if i == len(stem) || i == 0 { return numberedRecording{}, false }
 counter, err := strconv.ParseUint(stem[i:], 10, 64)
 if err != nil { return numberedRecording{}, false }
 return numberedRecording{
  ID:id,
  Directory:strings.ToLower(path.Dir(normalized)),
  Series:strings.ToLower(stem[:i] + base[dot:]),
  Number:counter,
 },true
}

// SequentialNeighbors returns only adjacent numeric counter pairs from an
// identical directory/prefix/extension family. Duplicated counters are
// ambiguous and excluded. Gaps do not imply missing recordings.
func SequentialNeighbors(p *projectstate.Project) ([]SequenceNeighbor,error) {
 if p==nil {return nil,fmt.Errorf("nil project")}
 if err:=p.Validate();err!=nil {return nil,err}
 groups:=map[string][]numberedRecording{}
 for id,r:=range p.Recordings {
  n,ok:=numberedSource(id,r.SourcePath)
  if !ok {continue}
  key:=n.Directory+"\x00"+n.Series
  groups[key]=append(groups[key],n)
 }
 keys:=make([]string,0,len(groups))
 for k:=range groups{keys=append(keys,k)}
 sort.Strings(keys)
 out:=make([]SequenceNeighbor,0)
 for _,key:=range keys{
  records:=groups[key]
  sort.Slice(records,func(i,j int)bool{
   if records[i].Number!=records[j].Number{return records[i].Number<records[j].Number}
   return records[i].ID<records[j].ID
  })
  for i:=1;i<len(records);i++{
   prev,next:=records[i-1],records[i]
   if next.Number!=prev.Number+1 || next.Number==records[i].Number && i+1<len(records)&&records[i+1].Number==next.Number || i>=2 && records[i-2].Number==prev.Number {
    continue
   }
   out=append(out,SequenceNeighbor{
    Before:prev.ID,After:next.ID,Directory:prev.Directory,Series:prev.Series,
    Basis:"same_directory_prefix_extension_adjacent_numeric_counter",
    Continuity:"unverified",
   })
  }
 }
 return out,nil
}

// SequencePriority assigns a scheduling preference to discovery hypotheses
// involving a target: adjacent in the same numbered series first, followed by
// other files in the same series, and then all other candidate references.
// It does not promote inferred chronological order to verified acoustic data.
func SequencePriority(p *projectstate.Project,targetID,otherID string)int{
 a,okA:=p.Recordings[targetID]
 b,okB:=p.Recordings[otherID]
 if !okA||!okB{return 2}
 x,okX:=numberedSource(targetID,a.SourcePath)
 y,okY:=numberedSource(otherID,b.SourcePath)
 if !okX||!okY||x.Directory!=y.Directory||x.Series!=y.Series{return 2}
 if x.Number<y.Number && y.Number-x.Number==1 ||
    y.Number<x.Number && x.Number-y.Number==1{return 0}
 return 1
}
