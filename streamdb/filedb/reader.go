package filedb

import (
    "os"
    "encoding/binary"
    "bytes"
    "errors"
)

type DataReader struct {
    offsetf *os.File            //The offset file (contains time stamps)
    dataf *os.File              //The data storage file (blob)
    size int64                  //The number of entries written when last checked
}

func (dr *DataReader) Close() {
    dr.offsetf.Close()
    dr.dataf.Close()
}

func (dr *DataReader) Len() (int64) {
    //Check the underlying file size - maybe it changed
    ostat,err := dr.offsetf.Stat()
    if ( err != nil) {
        return dr.size
    }
    dr.size = (ostat.Size()-8)/16   //16 bytes are written for each entry

    return dr.size
}

func GetReader(path string) (dr *DataReader, err error) {
    //Opens the offset and data files for append
    offsetf,err := os.OpenFile(path,os.O_RDONLY, 0666)
    if (err != nil) {
        return nil,err
    }
    dataf,err := os.OpenFile(path + ".data",os.O_RDONLY, 0666)
    if (err != nil) {
        offsetf.Close()
        return nil,err
    }

    dr = &DataReader{offsetf,dataf,0}
    dr.Len()    //Find the size of the file - update internal size

    return dr,nil
}

func (dr *DataReader) Read(index int64) (timestamp int64, data []byte, err error) {
    //Makes sure that the length is within range
    if (index >= dr.size) {
        if (index >= dr.Len()) {
            return 0,nil,errors.New("Index out of bounds")
        }
    }

    //The index is within bounds - read the offsetfile. The offsetfile is written: (startloc,timestamp,endloc)
    offsetbuffer := make([]byte, 8*3)
    dr.offsetf.ReadAt(offsetbuffer,2*8*index)

    //Decode the item
    var startloc int64
    var endloc int64
    buf := bytes.NewReader(offsetbuffer)
    binary.Read(buf,binary.LittleEndian,&startloc)
    binary.Read(buf,binary.LittleEndian,&timestamp)
    binary.Read(buf,binary.LittleEndian,&endloc)

    if (startloc > endloc) {
        return 0,nil,errors.New("File Corrupted")
    }
    if (startloc == endloc) {
        return timestamp,[]byte{},nil //If there is nothing to read, return empty bytes
    }

    databuffer := make([]byte,endloc-startloc)
    dr.dataf.ReadAt(databuffer,startloc)

    return timestamp,databuffer,nil
}

func (dr *DataReader) ReadTimestamp(index int64) (timestamp int64,err error) {
    //Makes sure that the length is within range
    if (index >= dr.size) {
        if (index >= dr.Len()) {
            return 0,errors.New("Index out of bounds")
        }
    }

    //The index is within bounds - read the offsetfile. The offsetfile is written: (startloc,timestamp,endloc)
    offsetbuffer := make([]byte, 8)
    dr.offsetf.ReadAt(offsetbuffer,2*8*index+8)

    buf := bytes.NewReader(offsetbuffer)
    binary.Read(buf,binary.LittleEndian,&timestamp)

    return timestamp,nil
}

func (dr *DataReader) ReadBatch(startindex int64,endindex int64) (timestamp []int64, data [][]byte, err error) {
    //Makes sure that the length is within range
    if (endindex > dr.size) {
        if (endindex > dr.Len()) {
            return nil,nil,errors.New("Index out of bounds")
        }
    }
    if (endindex <= startindex) {
        return nil,nil,errors.New("startindex and end index set incorrectly")
    }

    numread := endindex - startindex
    timestamp = make([]int64,numread)
    data = make([][]byte,numread)
    locs := make([]int64,numread+1) //The +1 is because there is one extra start location

    //The index is within bounds - read the offsetfile. The offsetfile is written: (startloc,timestamp,endloc)
    offsetbuffer := make([]byte, 16*numread+8)
    dr.offsetf.ReadAt(offsetbuffer,2*8*startindex)
    buf := bytes.NewReader(offsetbuffer)

    //Decode the offsetfile chunk
    for i := int64(0); i < numread; i++ {
        binary.Read(buf,binary.LittleEndian,&locs[i])
        binary.Read(buf,binary.LittleEndian,&timestamp[i])
    }
    binary.Read(buf,binary.LittleEndian,&locs[numread])

    if (locs[0] > locs[numread]) {
        return nil,nil,errors.New("File Corrupted")
    }

    //Read the data into the byte arrays
    databuffer := make([]byte,locs[numread]-locs[0])
    dr.dataf.ReadAt(databuffer,locs[0])
    for i := int64(0); i < numread; i++ {
        data[i] = databuffer[locs[i]-locs[0]:locs[i+1]-locs[0]]
    }

    return timestamp,data,nil
}

//Find the datapoint x such that the timestamp of x = inf(i> t),
//where i are datapoints and t is the given timestamp.
//Ie, it finds the first datapoint with a timestamp > given.
//TODO: This code makes no guarantees about nanosecond-level precision.
func (dr *DataReader) FindTime(timestamp int64) (index int64, err error) {
    //We do this shit logn style
    leftbound := int64(0)
    leftts,err := dr.ReadTimestamp(0)
    if err != nil {
        return 0,err
    }

    //If the timestamp is earlier than earliest datapoint
    if (leftts > timestamp) {
        return 0,nil
    }

    //If Len is 0, then we would have failed reading already
    rightbound := dr.Len()-1
    rightts,err := dr.ReadTimestamp(rightbound)
    if err != nil {
        return 0,err
    }

    if (rightts <= timestamp) {
        return dr.size,errors.New("Not within range") //Returns the answer along with a not in range error
    }

    for (rightbound - leftbound > 1) {
        midpoint := (leftbound + rightbound)/2
        ts,err := dr.ReadTimestamp(midpoint)
        if (err!= nil) {
            return 0,err
        }
        if (ts <= timestamp) {
            leftbound = midpoint
            leftts = ts
        } else {
            rightbound = midpoint
            rightts = ts
        }
    }
    return rightbound, nil
}


func (dr *DataReader) FindTimeRange(timestamp int64,timestamp2 int64) (index int64, index2 int64, err error) {
    //We could do this in a much faster way by using the fact that the time range number 1 limits the locations
    //  of timestamp2 but that can be done some time in the far,misty future.
    i1,err := dr.FindTime(timestamp)
    if (err!=nil) {
        return i1,-1,err
    }
    i2,err2 := dr.FindTime(timestamp2)
    if (err2!=nil) {
        return i1,i2,err2
    }
    return i1,i2,nil
}
