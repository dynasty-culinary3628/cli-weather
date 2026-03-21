package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// --- Unit conversion helpers ---

func cToF(c float64) float64    { return c*9/5 + 32 }
func kmhToMph(k float64) float64 { return k * 0.621371 }
func mToMiles(m float64) float64 { return m / 1609.344 }
func paToInHg(p float64) float64 { return p / 3386.389 }
func paToHPa(p float64) float64  { return p / 100.0 }

func degToCompass(deg float64) string {
	dirs := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE",
		"S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	i := int(math.Round(deg/22.5)) % 16
	if i < 0 {
		i += 16
	}
	return dirs[i]
}

// conditionIcon returns a weather emoji for a condition string.
func conditionIcon(cond string, daytime bool) string {
	c := strings.ToLower(cond)
	switch {
	case strings.Contains(c, "tornado"):
		return "🌪"
	case strings.Contains(c, "hurricane") || strings.Contains(c, "tropical storm"):
		return "🌀"
	case strings.Contains(c, "thunder"):
		return "⛈"
	case strings.Contains(c, "blizzard"):
		return "❄️"
	case strings.Contains(c, "snow") || strings.Contains(c, "flurr") || strings.Contains(c, "sleet"):
		return "🌨"
	case strings.Contains(c, "freezing") || strings.Contains(c, "ice"):
		return "🧊"
	case strings.Contains(c, "rain") || strings.Contains(c, "shower") || strings.Contains(c, "drizzle"):
		return "🌧"
	case strings.Contains(c, "fog") || strings.Contains(c, "mist") || strings.Contains(c, "haze"):
		return "🌫"
	case strings.Contains(c, "smoke") || strings.Contains(c, "dust") || strings.Contains(c, "sand"):
		return "💨"
	case strings.Contains(c, "partly") || strings.Contains(c, "few clouds") || strings.Contains(c, "mostly clear"):
		if daytime {
			return "⛅"
		}
		return "🌤"
	case strings.Contains(c, "overcast") || strings.Contains(c, "cloud"):
		return "☁️"
	case strings.Contains(c, "clear") || strings.Contains(c, "sunny") || strings.Contains(c, "fair"):
		if daytime {
			return "☀️"
		}
		return "🌙"
	default:
		if daytime {
			return "🌤"
		}
		return "🌙"
	}
}

// --- Data types ---

// LocationInfo holds the resolved location for a ZIP code.
type LocationInfo struct {
	Lat, Lon    float64
	City, State string
	ZipCode     string
}

// GridPoint holds the NWS grid point data needed to fetch forecasts.
type GridPoint struct {
	Office            string
	GridX, GridY      int
	ForecastURL       string
	ForecastHourlyURL string
	StationsURL       string
	ForecastZoneURL   string // e.g. https://api.weather.gov/zones/forecast/NYZ072
	TimeZone          string
	City, State       string
}

// ZoneForecastPeriod is one named period in the NWS zone text forecast.
type ZoneForecastPeriod struct {
	Name            string
	DetailedForecast string
}

// ForecastPeriod is one period (12-hour or 1-hour) in a forecast.
type ForecastPeriod struct {
	Number          int
	Name            string
	StartTime       time.Time
	EndTime         time.Time
	IsDaytime       bool
	Temperature     int
	TemperatureUnit string
	WindSpeed       string
	WindDirection   string
	ShortForecast   string
}

// Observation holds current weather conditions from a nearby station.
// Values are in SI units as returned by the API; convert for display.
type Observation struct {
	Timestamp       time.Time
	StationName     string
	TextDescription string
	TempC           *float64 // °C
	DewpointC       *float64 // °C
	RelHumidity     *float64 // %
	WindDirDeg      *float64 // degrees from north
	WindSpeedKmh    *float64 // km/h
	WindGustKmh     *float64 // km/h
	PressurePa      *float64 // Pascals
	VisibilityM     *float64 // meters
}

// Alert is an active NWS weather alert.
type Alert struct {
	Event    string
	Headline string
	Severity string
	Expires  time.Time
}

// WeatherData holds all fetched weather data for display.
type WeatherData struct {
	Location     LocationInfo
	GridPoint    GridPoint
	Conditions   Observation
	Forecast     []ForecastPeriod
	Hourly       []ForecastPeriod
	Alerts       []Alert
	ZoneForecast []ZoneForecastPeriod
	FetchedAt    time.Time
}

// --- HTTP client ---

// Client wraps http.Client with a User-Agent header for the weather.gov API.
type Client struct {
	http      *http.Client
	userAgent string
}

func newClient(ua string) *Client {
	return &Client{
		http:      &http.Client{Timeout: 30 * time.Second},
		userAgent: ua,
	}
}

func (c *Client) getJSON(url string, v interface{}) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/geo+json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// --- API methods ---

// LocationFromZip converts a US ZIP code to coordinates using zippopotam.us.
func (c *Client) LocationFromZip(zip string) (LocationInfo, error) {
	req, err := http.NewRequest("GET", "https://api.zippopotam.us/us/"+zip, nil)
	if err != nil {
		return LocationInfo{}, err
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return LocationInfo{}, fmt.Errorf("geocoding request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return LocationInfo{}, fmt.Errorf("ZIP code %q not found", zip)
	}
	if resp.StatusCode != http.StatusOK {
		return LocationInfo{}, fmt.Errorf("geocoding returned HTTP %d", resp.StatusCode)
	}

	var r struct {
		Places []struct {
			Name string `json:"place name"`
			Lon  string `json:"longitude"`
			Lat  string `json:"latitude"`
			St   string `json:"state abbreviation"`
		} `json:"places"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return LocationInfo{}, err
	}
	if len(r.Places) == 0 {
		return LocationInfo{}, fmt.Errorf("no location found for ZIP %s", zip)
	}

	p := r.Places[0]
	var lat, lon float64
	fmt.Sscanf(p.Lat, "%f", &lat)
	fmt.Sscanf(p.Lon, "%f", &lon)

	return LocationInfo{
		Lat:     lat,
		Lon:     lon,
		City:    p.Name,
		State:   p.St,
		ZipCode: zip,
	}, nil
}

// GridPointFromLatLon queries weather.gov /points to get grid coordinates.
func (c *Client) GridPointFromLatLon(lat, lon float64) (GridPoint, error) {
	u := fmt.Sprintf("https://api.weather.gov/points/%.4f,%.4f", lat, lon)

	var r struct {
		Properties struct {
			GridID              string `json:"gridId"`
			GridX               int    `json:"gridX"`
			GridY               int    `json:"gridY"`
			Forecast            string `json:"forecast"`
			ForecastHourly      string `json:"forecastHourly"`
			ObservationStations string `json:"observationStations"`
			ForecastZone        string `json:"forecastZone"`
			TimeZone            string `json:"timeZone"`
			RelativeLocation    struct {
				Properties struct {
					City  string `json:"city"`
					State string `json:"state"`
				} `json:"properties"`
			} `json:"relativeLocation"`
		} `json:"properties"`
	}

	if err := c.getJSON(u, &r); err != nil {
		return GridPoint{}, fmt.Errorf("weather.gov /points: %w", err)
	}

	p := r.Properties
	return GridPoint{
		Office:            p.GridID,
		GridX:             p.GridX,
		GridY:             p.GridY,
		ForecastURL:       p.Forecast,
		ForecastHourlyURL: p.ForecastHourly,
		StationsURL:       p.ObservationStations,
		ForecastZoneURL:   p.ForecastZone,
		TimeZone:          p.TimeZone,
		City:              p.RelativeLocation.Properties.City,
		State:             p.RelativeLocation.Properties.State,
	}, nil
}

// fetchPeriods is shared logic for forecast and hourly-forecast endpoints.
func (c *Client) fetchPeriods(u string) ([]ForecastPeriod, error) {
	var r struct {
		Properties struct {
			Periods []struct {
				Number          int    `json:"number"`
				Name            string `json:"name"`
				StartTime       string `json:"startTime"`
				EndTime         string `json:"endTime"`
				IsDaytime       bool   `json:"isDaytime"`
				Temperature     int    `json:"temperature"`
				TemperatureUnit string `json:"temperatureUnit"`
				WindSpeed       string `json:"windSpeed"`
				WindDirection   string `json:"windDirection"`
				ShortForecast   string `json:"shortForecast"`
			} `json:"periods"`
		} `json:"properties"`
	}

	if err := c.getJSON(u, &r); err != nil {
		return nil, err
	}

	out := make([]ForecastPeriod, 0, len(r.Properties.Periods))
	for _, p := range r.Properties.Periods {
		fp := ForecastPeriod{
			Number:          p.Number,
			Name:            p.Name,
			IsDaytime:       p.IsDaytime,
			Temperature:     p.Temperature,
			TemperatureUnit: p.TemperatureUnit,
			WindSpeed:       p.WindSpeed,
			WindDirection:   p.WindDirection,
			ShortForecast:   p.ShortForecast,
		}
		fp.StartTime, _ = time.Parse(time.RFC3339, p.StartTime)
		fp.EndTime, _ = time.Parse(time.RFC3339, p.EndTime)
		out = append(out, fp)
	}
	return out, nil
}

// floatVal matches the { "value": ..., "unitCode": ... } shape used in observations.
type floatVal struct {
	Value *float64 `json:"value"`
}

// LatestObservation fetches the most recent observation from the nearest station.
func (c *Client) LatestObservation(stationsURL string) (Observation, error) {
	var stations struct {
		Features []struct {
			Properties struct {
				StationIdentifier string `json:"stationIdentifier"`
				Name              string `json:"name"`
			} `json:"properties"`
		} `json:"features"`
	}
	if err := c.getJSON(stationsURL, &stations); err != nil {
		return Observation{}, fmt.Errorf("stations: %w", err)
	}
	if len(stations.Features) == 0 {
		return Observation{}, fmt.Errorf("no observation stations found")
	}

	id := stations.Features[0].Properties.StationIdentifier
	name := stations.Features[0].Properties.Name
	obsURL := fmt.Sprintf("https://api.weather.gov/stations/%s/observations/latest", id)

	var obs struct {
		Properties struct {
			Timestamp          string   `json:"timestamp"`
			TextDescription    string   `json:"textDescription"`
			Temperature        floatVal `json:"temperature"`
			Dewpoint           floatVal `json:"dewpoint"`
			RelativeHumidity   floatVal `json:"relativeHumidity"`
			WindDirection      floatVal `json:"windDirection"`
			WindSpeed          floatVal `json:"windSpeed"`
			WindGust           floatVal `json:"windGust"`
			BarometricPressure floatVal `json:"barometricPressure"`
			Visibility         floatVal `json:"visibility"`
		} `json:"properties"`
	}
	if err := c.getJSON(obsURL, &obs); err != nil {
		return Observation{}, fmt.Errorf("observation: %w", err)
	}

	p := obs.Properties
	o := Observation{
		StationName:     name,
		TextDescription: p.TextDescription,
		TempC:           p.Temperature.Value,
		DewpointC:       p.Dewpoint.Value,
		RelHumidity:     p.RelativeHumidity.Value,
		WindDirDeg:      p.WindDirection.Value,
		WindSpeedKmh:    p.WindSpeed.Value,
		WindGustKmh:     p.WindGust.Value,
		PressurePa:      p.BarometricPressure.Value,
		VisibilityM:     p.Visibility.Value,
	}
	o.Timestamp, _ = time.Parse(time.RFC3339, p.Timestamp)
	return o, nil
}

// ActiveAlerts fetches active NWS alerts for a point.
func (c *Client) ActiveAlerts(lat, lon float64) ([]Alert, error) {
	u := fmt.Sprintf("https://api.weather.gov/alerts/active?point=%.4f,%.4f", lat, lon)

	var r struct {
		Features []struct {
			Properties struct {
				Event    string `json:"event"`
				Headline string `json:"headline"`
				Severity string `json:"severity"`
				Expires  string `json:"expires"`
			} `json:"properties"`
		} `json:"features"`
	}
	if err := c.getJSON(u, &r); err != nil {
		return nil, err
	}

	out := make([]Alert, 0, len(r.Features))
	for _, f := range r.Features {
		a := Alert{
			Event:    f.Properties.Event,
			Headline: f.Properties.Headline,
			Severity: f.Properties.Severity,
		}
		a.Expires, _ = time.Parse(time.RFC3339, f.Properties.Expires)
		out = append(out, a)
	}
	return out, nil
}

// ZoneForecastText fetches the NWS zone text forecast (narrative paragraphs).
// zoneURL is the base zone URL from /points (e.g. .../zones/forecast/NYZ072);
// we append /forecast to get the forecast resource.
func (c *Client) ZoneForecastText(zoneURL string) ([]ZoneForecastPeriod, error) {
	if zoneURL == "" {
		return nil, nil
	}
	var r struct {
		Properties struct {
			Periods []struct {
				Name             string `json:"name"`
				DetailedForecast string `json:"detailedForecast"`
			} `json:"periods"`
		} `json:"properties"`
	}
	if err := c.getJSON(zoneURL+"/forecast", &r); err != nil {
		return nil, err
	}
	out := make([]ZoneForecastPeriod, 0, len(r.Properties.Periods))
	for _, p := range r.Properties.Periods {
		out = append(out, ZoneForecastPeriod{
			Name:             p.Name,
			DetailedForecast: p.DetailedForecast,
		})
	}
	return out, nil
}

// fetchResult is used for concurrent fetch operations.
type fetchResult[T any] struct {
	val T
	err error
}

// FetchAll concurrently fetches forecast, hourly, observations, alerts, and zone forecast.
func (c *Client) FetchAll(loc LocationInfo, gp GridPoint) (WeatherData, error) {
	d := WeatherData{Location: loc, GridPoint: gp, FetchedAt: time.Now()}

	fcCh := make(chan fetchResult[[]ForecastPeriod], 1)
	hCh := make(chan fetchResult[[]ForecastPeriod], 1)
	obsCh := make(chan fetchResult[Observation], 1)
	alCh := make(chan fetchResult[[]Alert], 1)
	zfCh := make(chan fetchResult[[]ZoneForecastPeriod], 1)

	go func() {
		v, e := c.fetchPeriods(gp.ForecastURL)
		fcCh <- fetchResult[[]ForecastPeriod]{v, e}
	}()
	go func() {
		v, e := c.fetchPeriods(gp.ForecastHourlyURL)
		hCh <- fetchResult[[]ForecastPeriod]{v, e}
	}()
	go func() {
		v, e := c.LatestObservation(gp.StationsURL)
		obsCh <- fetchResult[Observation]{v, e}
	}()
	go func() {
		v, e := c.ActiveAlerts(loc.Lat, loc.Lon)
		alCh <- fetchResult[[]Alert]{v, e}
	}()
	go func() {
		v, e := c.ZoneForecastText(gp.ForecastZoneURL)
		zfCh <- fetchResult[[]ZoneForecastPeriod]{v, e}
	}()

	fc := <-fcCh
	h := <-hCh
	obs := <-obsCh
	al := <-alCh
	zf := <-zfCh

	if fc.err != nil {
		return d, fmt.Errorf("forecast: %w", fc.err)
	}
	if h.err != nil {
		return d, fmt.Errorf("hourly forecast: %w", h.err)
	}
	if obs.err != nil {
		return d, fmt.Errorf("observations: %w", obs.err)
	}
	// Alerts and zone forecast failures are non-fatal — continue without them.
	d.Forecast = fc.val
	d.Hourly = h.val
	d.Conditions = obs.val
	d.Alerts = al.val
	d.ZoneForecast = zf.val
	return d, nil
}
