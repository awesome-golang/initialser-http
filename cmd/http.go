package cmd
import (
	"gopkg.in/urfave/cli.v2"
	"github.com/gorilla/mux"
	"net/http"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"path/filepath"
	"strings"
	"github.com/leonlau/initialser"
	"strconv"
	"errors"
	"io/ioutil"
	"os"
	"github.com/leonlau/initialser-http/cache"
	"crypto/md5"
	"encoding/hex"
)

var CmdHttp = &cli.Command{
	Name:"http",
	Usage:"start a http server",
	Action:runHttp,
	Flags:[]cli.Flag{
		&cli.IntFlag{
			Name:"port",
			Aliases:[]string{"p"},
			Value:80,
			Usage:"set port,-p 80",
		},
		&cli.IntFlag{
			Name:"max-bg-size",
			Value:1024,
			Usage:"set max background size,-max-bg-size 1024",
		},
		&cli.IntFlag{
			Name:"max-f-size",
			Value:800,
			Usage:"set max font size,-max-f-size 1024",
		},
		&cli.StringFlag{
			Name:"cache",
			Value:"F",
			Usage:"enable disk cache,-cache T",
		},
		&cli.StringFlag{
			Name:"debug",
			Value:"F",
			Usage:"enable debug log,-deubg T",
		},
		&cli.StringFlag{
			Name:"dir",
			Value:"resource",
			Aliases:[]string{"d"},
			Usage:"set dir,-dir resourse",
		},
	},

}

const (
	fileNamePathKey = "file_name"
)
func init() {
	log.SetLevel(log.WarnLevel)
}

var (
	conf = newConfig(9527)
	kv cache.KV
)

type config  struct {
	maxFontSize int
	maxBgSize   int
	port        int
	dir         string
	cache       bool
}

func newConfig(port int) *config {
	return &config{
		port:port,
		dir:"resource",
	}
}

func (c *config)String() string {
	return fmt.Sprintf(`
maxFontSize:%d
maxBgSize:%d
port:%d
dir:%s
cache:%v
	`,
		c.maxFontSize,
		c.maxBgSize,
		c.port,
		c.dir,
		c.cache)
}

func runHttp(c *cli.Context) error {
	conf.port = c.Int("port")
	conf.dir = c.String("dir")
	conf.maxBgSize = c.Int("max-bg-size")
	conf.maxFontSize = c.Int("max-f-size")
	conf.cache = c.Bool("cache")
	if c.Bool("debug") {
		log.SetLevel(log.DebugLevel)
	}
	addr := fmt.Sprintf(":%d", conf.port);
	r := mux.NewRouter()
	r.HandleFunc("/", homeHandler);
	r.HandleFunc(fmt.Sprintf("/{%s}", fileNamePathKey), avatarHandler);
	log.Debug("app start ", addr)
	conf.dir = os.ExpandEnv(conf.dir);
	log.Debug(conf.String())
	//
	//	kv = cache.NewSimpleDiskCache(filepath.Join(conf.dir, "initial"), func(key string) []string {
	//		if len(key) == 32 {
	//			return []string{key[:8], key[8:16], key[16:24], key[24:]};
	//		}
	//		return []string{key};
	//	})
	kv = cache.NewBoltCache(filepath.Join(conf.dir, "initial"));
	initialser.OnlyPath(filepath.Join(conf.dir, "/fonts/*"))
	return http.ListenAndServe(addr, r)

}



//homeHandler home router
func homeHandler(w http.ResponseWriter, req *http.Request) {
	data, err := ioutil.ReadFile(filepath.Join(conf.dir, "index.html"))
	if err != nil {
		log.Debug(err)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("404"));
		return
	}
	w.Write(data)
}
//avatarHandler server avatar
func avatarHandler(w http.ResponseWriter, req *http.Request) {
	// parse path name
	text, ext := parseFileName(req);
	switch ext {
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
		na := initialser.NewAvatar(text);
		//setCacheControl(w, na.Key());
		if badReq(w, parseParamTo(na, req)) {
			return;
		}
		fmt.Fprint(w, na.Svg())
		return;
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	case ".png", "":
		w.Header().Set("Content-Type", "image/png")
	default:
		badReq(w, errors.New("not support ext " + ext))
		return;
	}
	avatar := initialser.NewAvatar(text)
	avatar.Ext = ext[1:]
	// parse query param to avatar
	err := parseParamTo(avatar, req);
	if badReq(w, err) {
		return;
	}
	d, err := initialser.NewDrawer(avatar)

	key := md5hash(avatar.Key())
	if !badReq(w, err) && !badReq(w, adapterResponse(key, w, d)) {
		//setCacheControl(w, hex.EncodeToString(key));
	}
}

func md5hash(key string) []byte {
	h := md5.New()
	h.Write([]byte(key))
	return h.Sum(nil)
}

//adapterResponse
func adapterResponse(key []byte, w http.ResponseWriter, d *initialser.Drawer) error {
	if conf.cache {
		var (
			data []byte
			err error
		)
		if data, ok := kv.Get(key); ok {
			_, err = w.Write(data)
			return err
		}
		data, err = d.DrawToBytes()
		if err == nil {
			log.Debug("set cache ", hex.EncodeToString(key))
			log.Debug(kv.Set(key, data))
		}
		_, err = w.Write(data)
		return err
	}
	return d.DrawToWriter(w)
}




//parseFileName parse url file name
func parseFileName(req *http.Request) (title string, ext string) {
	fileName := mux.Vars(req)[fileNamePathKey]
	ext = filepath.Ext(fileName)
	ext = strings.ToLower(ext)
	title = strings.TrimSuffix(fileName, ext)
	return
}


func setCacheControl(w http.ResponseWriter, etag string) {
	w.Header().Set("Cache-Control", "max-age=2592000") //second 30days
	w.Header().Set("Etag", etag);

}
//parseParam  ?bg=#dd00ff&s=200&f=宋体&fs=120&c=#020319
func parseParamTo(avatar *initialser.Avatar, req *http.Request) error {
	q := req.URL.Query()
	avatar.Font = ifBlankDefault(q.Get("f"), "Hiragino_Sans_GB_W3")
	avatar.Color = ifBlankDefault(q.Get("c"), avatar.Color)
	avatar.Background = ifBlankDefault(q.Get("bg"), avatar.Background)
	if q.Get("s") != "" {
		if size, err := strconv.Atoi(q.Get("s")); err != nil {
			return errors.New("s is not a valid int number")
		}else {
			avatar.Size = size
		}
	}
	if q.Get("fs") != "" {
		if fs, err := strconv.Atoi(q.Get("fs")); err != nil {
			return errors.New("fs is not a valid int number")
		}else {
			avatar.FontSize = fs
		}
	}
	if avatar.Size > conf.maxBgSize {
		return errors.New("s has exceeded the maximum limit ")
	}
	if avatar.FontSize > conf.maxFontSize {
		return errors.New("fs has exceeded the maximum limit")
	}

	return nil
}
//ifBlankDefault default value
func ifBlankDefault(str string, defStr string) string {
	if str == "" {
		return defStr
	}
	return str
}

//badReq err exist ,return bad request
func badReq(w http.ResponseWriter, err error) bool {
	if err == nil {
		//cache
		return false
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusBadRequest)
	w.Write([]byte(err.Error()))
	return true
}



