package main

import (
    "github.com/bwmarrin/discordgo"
    "fmt"
    "os"
    "os/signal"
    "syscall"
    "bufio"
    "strings"
    "net/http"
    "encoding/json"
    "io/ioutil"
    "regexp"
    "strconv"
    "errors"
    "sort"
    "time"
)

const poolsJSON string = "https://raw.githubusercontent.com/turtlecoin/" +
                         "turtlecoin-pools-json/master/turtlecoin-pools.json"

type Pool struct {
    Url string `json:??,string`
    Api string `json:url,string`
}

type Pools map[string]*Pool

var globalPools Pools
var globalHeights map[string]int
var globalHeight int

func main() {
    discord, err := startup()
    
    if err != nil {
        return
    }

    globalPools, err = getPools()

    fmt.Println(len(globalPools))

    if err != nil {
        return
    }

    globalHeights = getHeights(globalPools)

    globalHeight = median(getValues(globalHeights))

    fmt.Println("Bot started!")

    /* Update the height and pools in the background */
    go heightWatcher()
    go poolUpdater()

    sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
    <-sc

    fmt.Println("Shutdown requested.")

    discord.Close()

    fmt.Println("Shutdown.")
}

/* Update the height and median height every 30 secs */
func heightWatcher() {
    for {
        time.Sleep(time.Second * 30)

        globalHeights = getHeights(globalPools)
        globalHeight = median(getValues(globalHeights))
    }
}

/* Update the pools json every hour */
func poolUpdater() {
    for {
        time.Sleep(time.Hour)

        tmpPools, err := getPools()
        
        if err != nil {
            return
        }

        globalPools = tmpPools
    }
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
    /* Ignore our own messages */
    if m.Author.ID == s.State.User.ID {
        return
    }

    if m.Content == ".heights" {
        heightsPretty := "```\nAll known pool heights:\n\n"

        for k, v := range globalHeights {
            heightsPretty += fmt.Sprintf("%-25s %d\n", k, v)
        }

        heightsPretty += "```"

        s.ChannelMessageSend(m.ChannelID, heightsPretty)

        return
    }

    if m.Content == ".help" {
        helpCommand := fmt.Sprintf("```\nAvailable commands:\n\n" +
                   ".help           Display this help message\n" +
                   ".heights        Display the heights of all known pools\n" +
                   ".height         Display the median height of all pools\n" +
                   ".height <pool>  Display the height of <pool>\n" +
                   ".claim <pool>   Claim the pool <pool> as your pool```")


        s.ChannelMessageSend(m.ChannelID, helpCommand)

        return
    }

    if m.Content == ".height" {
        s.ChannelMessageSend(m.ChannelID, 
                             fmt.Sprintf("```Median pool height:\n\n%d```", 
                                         globalHeight))

        return
    }
}

func getValues(heights map[string]int) []int {
    values := make([]int, 0)

    for _, v := range heights {
        values = append(values, v)
    }

    return values
}

func median(heights []int) int {
    sort.Ints(heights)

    half := len(heights) / 2
    median := heights[half]

    if len(heights) % 2 == 0 {
        median = (median + heights[half-1]) / 2
    }

    return median
}

func getHeights (pools Pools) map[string]int {
    heights := make(map[string]int)

    for _, v := range pools {
        height, err := getPoolHeight(v.Api)

        if err == nil {
            heights[v.Url] = height
        }
    }

    return heights
}

func getPoolHeight (apiURL string) (int, error) {
    statsURL := apiURL + "stats"

    resp, err := http.Get(statsURL)

    if err != nil {
        fmt.Printf("Failed to download stats from %s! Error: %s\n", 
                    statsURL, err)
        return 0, err
    }

    defer resp.Body.Close()

    body, err := ioutil.ReadAll(resp.Body)

    if err != nil {
        fmt.Printf("Failed to download stats from %s! Error: %s\n",
                    statsURL, err)
        return 0, err
    }

    re := regexp.MustCompile(".*\"height\":(\\d+).*")

    height := re.FindStringSubmatch(string(body))

    if len(height) < 2 {
        fmt.Println("Failed to parse height from", statsURL)
        return 0, errors.New("Couldn't parse height")
    }

    i, err := strconv.Atoi(height[1])

    if err != nil {
        fmt.Println("Failed to convert height into int! Error:", err)
        return 0, err
    }

    return i, nil
}

/* Thanks to https://stackoverflow.com/a/48716447/8737306 */
func (p *Pools) UnmarshalJSON (data []byte) error {
    var transient = make(map[string]*Pool)

    err := json.Unmarshal(data, &transient)

    if err != nil {
        return err
    }

    /* Not sure why this is parsing kinda backwards... */
    for k, v := range transient {
        v.Api = v.Url
        v.Url = k
        (*p)[k] = v
    }

    fmt.Println("Got pools json!")

    return nil
}

func getPools() (Pools, error) {
    var pools Pools = make(map[string]*Pool)

    resp, err := http.Get(poolsJSON)

    if err != nil {
        fmt.Println("Failed to download pools json! Error:", err)
        return pools, err
    }

    defer resp.Body.Close()

    body, err := ioutil.ReadAll(resp.Body)

    if err != nil {
        fmt.Println("Failed to download pools json! Error:", err)
        return pools, err
    }

    err = pools.UnmarshalJSON(body)

    if err != nil {
        fmt.Println("Failed to parse pools json! Error:", err)
        return pools, err
    }

    return pools, nil
}

func startup() (*discordgo.Session, error) {
    var discord *discordgo.Session

    token, err := getToken()

    if err != nil {
        fmt.Println("Failed to get token! Error:", err)
        return discord, err
    }

    discord, err = discordgo.New("Bot " + token)

    if err != nil {
        fmt.Println("Failed to init bot! Error:", err)
        return discord, err
    }

    discord.AddHandler(messageCreate)

    err = discord.Open()

    if err != nil {
        fmt.Println("Error opening connection! Error:", err)
        return discord, err
    }

    fmt.Println("Connected to discord!")

    return discord, nil
}

func getToken() (string, error) {
    file, err := os.Open("token.txt")

    defer file.Close()

    if err != nil {
        return "", err
    }

    reader := bufio.NewReader(file)

    line, err := reader.ReadString('\n')

    if err != nil {
        return "", err
    }

    line = strings.TrimSuffix(line, "\n")

    return line, nil
}
