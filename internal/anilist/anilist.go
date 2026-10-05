package anilist

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
)

const anilistBaseURL = "https://graphql.anilist.co/"

type Title struct {
	English string
	Romaji  string
}

type StreamingEpisode struct {
	Title string
}

type Media struct {
	Id                int
	Title             Title
	Description       string
	Status            string
	Episodes          int
	AverageScore      int
	CoverImage        CoverImage
	StartDate         StartDate
	Studios           StudioConnection
	NextAiringEpisode *AiringSchedule
	StreamingEpisodes []StreamingEpisode
}

type AiringSchedule struct {
	Episode  int
	AiringAt int64
}

type StartDate struct {
	Year  int
	Month int
	Day   int
}

type StudioConnection struct {
	Nodes []Studio
}

type Studio struct {
	Name string
}
type CoverImage struct {
	Medium string
	Color  string
}

type MediaMinimal struct {
	Id    int
	Title Title
}

type Page struct {
	Media []MediaMinimal
}

type postParam struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

type postError struct {
	Message string
	Status  int
}

type postDataFindAnime struct {
	Media Media `json:"Media"`
}

type postDataSearchAnime struct {
	Page Page `json:"Page"`
}

type postReply[T any] struct {
	Errors []postError
	Data   *T
}

func executeQuery[T any](
	client *http.Client,
	param postParam,
) (*T, error) {
	body, err := json.Marshal(param)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(
		"POST",
		anilistBaseURL,
		bytes.NewBuffer(body),
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var reply postReply[T]

	if err := json.NewDecoder(res.Body).Decode(&reply); err != nil {
		return nil, err
	}

	if len(reply.Errors) != 0 {
		return nil, errors.New(reply.Errors[0].Message)
	}

	return reply.Data, nil
}

func GetAnime(
	client *http.Client,
	id int,
) (*Media, error) {
	q := `
		query ($id: Int) {
			Media(id: $id) {
				id
				title {
					romaji
					english
				}
				description
				status
				episodes
				averageScore
				coverImage {
					medium
					color
				}
				startDate {
					year
					month
					day
				}
				studios {
					nodes {
						name
					}
				}
				nextAiringEpisode {
					episode
					airingAt
				}
				streamingEpisodes {
					title
				}
			}
		}
	`

	param := postParam{
		Query: q,
		Variables: map[string]interface{}{
			"id": id,
		},
	}

	res, err := executeQuery[postDataFindAnime](client, param)
	if err != nil {
		return nil, err
	}

	return &res.Media, nil
}

func SearchAnime(
	client *http.Client,
	search string,
	limit int,
) (*Page, error) {
	q := `
		query ($search: String, $limit: Int) {
			Page(page: 1, perPage: $limit) {
				media(search: $search, type: ANIME) {
					id
					title {
						romaji
						english
					}
				}
			}
		}
	`

	param := postParam{
		Query: q,
		Variables: map[string]interface{}{
			"search": search,
			"limit":  limit,
		},
	}

	res, err := executeQuery[postDataSearchAnime](client, param)
	if err != nil {
		return nil, err
	}

	return &res.Page, nil
}

func SearchSeasonAnime(
	client *http.Client,
	season string,
	year int,
	limit int,
) (*Page, error) {
	q := `
		query (
			$season: MediaSeason
			$year: Int
			$limit: Int
		) {
			Page(
				page: 1
				perPage: $limit
			) {
				media(
					season: $season
					seasonYear: $year
					type: ANIME
					sort: START_DATE
				) {
					id
					title {
						romaji
						english
					}
				}
			}
		}
	`

	param := postParam{
		Query: q,
		Variables: map[string]interface{}{
			"season": season,
			"year":   year,
			"limit":  limit,
		},
	}

	res, err := executeQuery[postDataSearchAnime](
		client,
		param,
	)
	if err != nil {
		return nil, err
	}

	return &res.Page, nil
}
