package crawler

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/sidesbutgithub/tftStats/matchCrawler/internal/database"
	"github.com/sidesbutgithub/tftStats/matchCrawler/internal/databaseClients"
	"github.com/sidesbutgithub/tftStats/matchCrawler/internal/models"
	"github.com/sidesbutgithub/tftStats/matchCrawler/internal/utils"
)

//store data locally before writing as bulk insert queries significantly faster

type Crawler struct {
	Mu *sync.Mutex
	Wg *sync.WaitGroup

	Rdb        *databaseClients.RedisDB
	CurrData   []database.BulkInsertUnitsParams
	RiotApiKey string
	NumWorkers int
}

func (crawlerInst *Crawler) AddMatchIfNotVisited(matchId string) (bool, error) {
	crawlerInst.Mu.Lock()
	defer crawlerInst.Mu.Unlock()

	visited, err := crawlerInst.Rdb.CheckMatchVisited(matchId)
	if err != nil {
		log.Print("Error checking if match was visited")
		return false, err
	}
	if visited {
		return false, nil
	}
	err = crawlerInst.Rdb.EnqueueMatch(matchId)
	if err != nil {
		log.Print("Error enquing match")
		return false, err
	}
	err = crawlerInst.Rdb.MarkMatchVisited(matchId)
	if err != nil {
		log.Print("Error marking match as visited")
		return false, err
	}
	return true, nil
}

func (crawlerInst *Crawler) AddPlayerIfNotVisited(puuid string) (bool, error) {
	crawlerInst.Mu.Lock()
	defer crawlerInst.Mu.Unlock()

	visited, err := crawlerInst.Rdb.CheckPlayerVisited(puuid)
	if err != nil {
		log.Print("Error checking if player was visited")
		return false, err
	}
	if visited {
		return false, nil
	}
	err = crawlerInst.Rdb.EnqueuePlayer(puuid)
	if err != nil {
		log.Print("Error enquing player")
		return false, err
	}
	err = crawlerInst.Rdb.MarkPlayerVisited(puuid)
	if err != nil {
		log.Print("Error marking player as visited")
		return false, err
	}
	return true, nil
}

// adds the data of a given match to the database and adds all the participants of that match
func (crawlerInst *Crawler) GetMatchDataFromMatchID(matchID string) error {
	defer crawlerInst.Wg.Done()
	reqAddress := fmt.Sprintf("https://americas.api.riotgames.com/tft/match/v1/matches/%s?api_key=%s", matchID, crawlerInst.RiotApiKey)

	b, err := utils.HandleHttpGetReqWithRetries(reqAddress, 5, 5)
	if err != nil {
		log.Print("Failed to read response body")
		return err
	}

	var bodyData models.MatchResponse

	err = json.Unmarshal(b, &bodyData)
	if err != nil {
		log.Print("Failed to unmarshall body data")
		return err
	}

	for _, participant := range bodyData.Info.Participants {
		_, err := crawlerInst.AddPlayerIfNotVisited(participant.Puuid)
		if err != nil {
			log.Print("error adding player to queue and visited set")
			return err
		}
		crawlerInst.Mu.Lock()
		for _, unit := range participant.Units {
			//insert to slice within object to bulk write later
			crawlerInst.CurrData = append(crawlerInst.CurrData, database.BulkInsertUnitsParams{
				Unitname:  unit.CharacterID,
				Starlevel: int16(unit.Tier),
				Items:     unit.ItemNames,
				Placement: int16(participant.Placement),
			})
		}
		crawlerInst.Mu.Unlock()
	}
	return nil
}

// inserts the last 20 matches of the given puuid into the matches queue and marks them as visited if not already visited
func (crawlerInst *Crawler) GetMatchesFromPuuid(puuid string) {
	defer crawlerInst.Wg.Done()
	reqAddress := fmt.Sprintf("https://americas.api.riotgames.com/tft/match/v1/matches/by-puuid/%s/ids?start=0&count=20&api_key=%s", puuid, crawlerInst.RiotApiKey)

	b, err := utils.HandleHttpGetReqWithRetries(reqAddress, 5, 5)
	if err != nil {
		log.Print(err)
		log.Fatal("Failed to read response body")
	}

	var bodyData []string

	err = json.Unmarshal(b, &bodyData)
	if err != nil {
		log.Print(b)
		log.Print(err)
		log.Fatal("Failed to unmarshall body data")
	}

	for _, matchId := range bodyData {
		_, err := crawlerInst.AddMatchIfNotVisited(matchId)
		if err != nil {
			log.Print(err)
			log.Fatal("error adding player to queue and visited set")
		}
	}
}
