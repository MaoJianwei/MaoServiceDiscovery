package branch

import (
	pb "MaoServerDiscovery/grpc.maojianwei.com/server/discovery/api"
	parent "MaoServerDiscovery/util"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	influxdb2Api "github.com/influxdata/influxdb-client-go/v2/api"

	"context"
	"fmt"
	"google.golang.org/grpc"
	"net"
	"os/exec"
	"strconv"
	"time"
)


/*
    return v6In,v6Out,error
 */
func getNat66GatewayData() (uint64, uint64, error) {
	ccc := exec.Command("/bin/bash", "-c", "ip6tables -nvxL FORWARD | grep MaoIPv6In | awk '{printf $2}'")
	ininin, err := ccc.CombinedOutput()
	if err != nil {
		parent.MaoLog(parent.ERROR, "Fail to get MaoIPv6In, " + err.Error())
		return 0, 0, err
	}
	ccc = exec.Command("/bin/bash", "-c", "ip6tables -nvxL FORWARD | grep MaoIPv6Out | awk '{printf $2}'")
	outoutout, err := ccc.CombinedOutput()
	if err != nil {
		parent.MaoLog(parent.ERROR, "Fail to get MaoIPv6Out, " + err.Error())
		return 0, 0, err
	}
	v6In, err := strconv.ParseUint(string(ininin), 10, 64)
	if err != nil {
		parent.MaoLog(parent.ERROR, "Fail to parse MaoIPv6In, " + err.Error())
		return 0, 0, err
	}
	v6Out, err := strconv.ParseUint(string(outoutout), 10, 64)
	if err != nil {
		parent.MaoLog(parent.ERROR, "Fail to parse MaoIPv6Out, " + err.Error())
		return 0, 0, err
	}
	parent.MaoLog(parent.DEBUG, fmt.Sprintf("v6In: %d , v6Out: %d", v6In, v6Out))
	return v6In, v6Out, nil
}

func nat66UploadInfluxdb(writeAPI *influxdb2Api.WriteAPI, v6In uint64, v6Out uint64) {
	// write point asynchronously
	(*writeAPI).WritePoint(
		influxdb2.NewPointWithMeasurement("NAT66_Gateway").
			AddTag("Geo", "Beijing-HQ").
			AddField("v6In", v6In).
			AddField("v6Out", v6Out).
			SetTime(time.Now()))
	// Not flush writes, avoid blocking my thread, then the lib's thread will block itself.
	//(*writeAPI).Flush()
}

func RunGeneralClient(report_server_addr *net.IP, report_server_port uint32, report_interval uint32, silent bool,
	nat66Gateway bool, nat66Persistent bool, influxdbUrl string, influxdbOrgBucket string, influxdbToken string) {

	var influxdbClient influxdb2.Client
	var influxdbWriteAPI influxdb2Api.WriteAPI
	if nat66Persistent == true {
		parent.MaoLog(parent.INFO, "Initiate influxdb client ...")
		influxdbClient = influxdb2.NewClient(influxdbUrl, influxdbToken)
		defer influxdbClient.Close()
		influxdbWriteAPI = influxdbClient.WriteAPI(influxdbOrgBucket, influxdbOrgBucket)
	}

	parent.MaoLog(parent.INFO, "Connect to center ...")
	for {
		serverAddr := parent.GetAddrPort(report_server_addr, report_server_port)
		parent.MaoLog(parent.INFO, fmt.Sprintf("Connect to %s ...", serverAddr))

		ctx, cancelCtx := context.WithTimeout(context.Background(), 3 * time.Second)
		connect, err := grpc.DialContext(ctx, serverAddr, grpc.WithInsecure(), grpc.WithBlock())
		if err != nil {
			parent.MaoLog(parent.WARN, fmt.Sprintf("Retry, %s ...", err))
			continue
		}
		cancelCtx()
		parent.MaoLog(parent.INFO, "Connected.")

		client := pb.NewMaoServerDiscoveryClient(connect)
		streamClient, err := client.Report(context.Background())
		if err != nil {
			parent.MaoLog(parent.ERROR, fmt.Sprintf("Fail to get streamClient, %s", err))
			continue
		}
		parent.MaoLog(parent.INFO, "Got StreamClient.")

		count := 1
		for {
			dataOk := true
			hostname, _ := parent.GetHostname()
			if err != nil {
				hostname = "Mao-Unknown"
				dataOk = false
			}

			ips, _ := parent.GetUnicastIp()
			if err != nil {
				ips = []string{"Mao-Fail", err.Error()}
				dataOk = false
			}

			parent.MaoLog(parent.DEBUG, fmt.Sprintf("%d: To send", count))
			report := &pb.ServerReport{
				Ok:          dataOk,
				Hostname:    hostname,
				Ips:         ips,
				NowDatetime: time.Now().String(),
			}
			if nat66Gateway {
				v6In, v6Out, err := getNat66GatewayData()
				if err == nil {
					report.AuxData = fmt.Sprintf("{ \"v6In\":%d, \"v6Out\":%d}", v6In, v6Out)
					if nat66Persistent {
						nat66UploadInfluxdb(&influxdbWriteAPI, v6In, v6Out)
					}
				}
			}

			err := streamClient.Send(report)
			if err != nil {
				parent.MaoLog(parent.ERROR, fmt.Sprintf("Fail to report, %s", err))
				break
			}
			if silent == false {
				parent.MaoLog(parent.INFO, fmt.Sprintf("ServerReport - %v", report))
			}
			parent.MaoLog(parent.DEBUG, fmt.Sprintf("%d: Sent", count))

			count++
			time.Sleep(time.Duration(report_interval) * time.Millisecond)
		}
		time.Sleep(1 * time.Second)
	}
}
