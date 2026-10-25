package core

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const huangguoNewSiteHome = `<html><body>
<div class="hg-card-grid is-active" data-channel-panel="latest" data-panel-total="100">
<div class="hg-drama-card" data-track-id="10219" data-track-title="转生降服高冷女魔头" data-track-type-name="AI成人短剧">
<div class="hg-drama-card__cover"><a href="/video/10219/"><img data-src="https://pic.example/10219.jpg"></a>
<span class="hg-drama-card__badge">新上架</span><span class="hg-drama-card__score">9.2分</span>
<span class="hg-drama-card__episode" data-ep-base="更新至6集">1天前更新至6集</span></div>
<div class="hg-drama-card__body"><h3 class="hg-drama-card__title"><a href="/video/10219/">转生降服高冷女魔头</a></h3>
<p class="hg-drama-card__desc">简介文字</p><div class="hg-drama-card__tags"><a class="hg-tag" href="/tag/xianxia/">仙侠</a></div></div>
</div></div>
<div class="hg-card-grid" data-channel-panel="hot" hidden>
<div class="hg-drama-card" data-track-id="999" data-track-title="热播推荐"><a href="/video/999/"><img data-src="https://pic.example/999.jpg"></a></div>
</div>
<nav class="hg-pager"><a class="hg-pager__arrow" href="/ai-duanju/2/" rel="next" title="下一页">下一页</a></nav>
</body></html>`

const huangguoNewSiteDetail = `<html><head><title>测试剧 - 短剧视频在线观看 | 黄果短剧</title>
<meta name="description" content="详情简介"></head><body>
<h1>测试剧</h1>
<main class="main">
<a class="hg-web-play__action" data-web-play-ep-next href="/video/10219/ep-2/">下一集</a>
<a class="hg-web-play__ep is-active" href="/video/10219/" data-ep-id="1">01</a>
<a class="hg-web-play__ep" href="/video/10219/ep-2/" data-ep-id="2">02</a>
<a class="hg-web-play__ep" href="/video/10219/ep-3/" data-ep-id="3">03</a>
<a class="hg-web-play__ep" href="/video/10219/ep-4/" data-ep-id="4">04</a>
<a class="hg-web-play__ep" href="/video/10219/ep-5/" data-ep-id="5">05</a>
<a class="hg-web-play__ep" href="/video/10219/ep-6/" data-ep-id="6">06</a>
<script id="videoInitialData" type="application/json">{"id":"10219","title":"测试剧","description":"简介","coverSrc":"https://pic.example/cover.jpg","views":"1.2万","time":"2026-10-23 12:00:00","tags":["仙侠","穿越"],"ep":1,"videoSrc":"https://media.example/one.m3u8?x=1\u0026y=2","epPlaySrcs":{"1":"https://media.example/one.m3u8?x=1\u0026y=2"}}</script>
</main></body></html>`

func TestHuangguoAINewSiteCatalogAndEpisodes(t *testing.T) {
	panel := huangguoAIChannelPanelHTML(huangguoNewSiteHome)
	items := parseHuangguoAIDramaCards(panel, "https://huangguoai.com/ai-duanju/", "AI 短剧")
	if len(items) != 1 || items[0].ID != "huangguoai:10219" || items[0].Title != "转生降服高冷女魔头" ||
		items[0].EpisodeCount != 6 || items[0].Remark != "新上架 更新至6集" || len(items[0].Tags) != 1 {
		t.Fatalf("new huangguo cards parsed incorrectly: %+v", items)
	}
	if !huangguoAIHasNextPage(huangguoNewSiteHome) {
		t.Fatal("new huangguo pager was not detected")
	}
	if path := huangguoAIHTMLAddress("https://huangguoai.com", "ai-duanju", 3); path != "https://huangguoai.com/ai-duanju/3/" {
		t.Fatalf("unexpected huangguo catalog path: %s", path)
	}
	episodes := parseHuangguoAIEpisodes(huangguoNewSiteDetail, "https://huangguoai.com/video/10219/", "10219")
	if len(episodes) != 6 {
		t.Fatalf("unexpected new huangguo episode count: %d %+v", len(episodes), episodes)
	}
	for index, episode := range episodes {
		if episode.Index != index+1 || episode.Title != "第"+string(rune('1'+index))+"集" ||
			episode.URL != "https://huangguoai.com/video/10219/"+map[bool]string{true: "", false: "ep-" + string(rune('1'+index)) + "/"}[index == 0] {
			t.Fatalf("new huangguo episode %d parsed incorrectly: %+v", index+1, episode)
		}
	}
	if !huangguoAIVideoPageMatches(huangguoNewSiteDetail, "10219") {
		t.Fatal("new huangguo detail was not recognized")
	}
}

func TestHuangguoAINewSiteDetailAndPlayback(t *testing.T) {
	var sawLegacy bool
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/video/10219/":
			return sourceFixtureResponse(request, http.StatusOK, huangguoNewSiteDetail), nil
		case "/detail/10219/":
			sawLegacy = true
			return sourceFixtureResponse(request, http.StatusNotFound, "not found"), nil
		case "/one.m3u8":
			return sourceFixtureResponse(request, http.StatusOK, "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\nsegment.ts\n#EXT-X-ENDLIST\n"), nil
		}
		return sourceFixtureResponse(request, http.StatusNotFound, "not found"), nil
	})
	drama, chapters, err := d.fetchHuangguoAIDetail(context.Background(), "10219")
	if err != nil || sawLegacy || drama.Title != "测试剧" || drama.Desc != "简介" || len(chapters) != 6 ||
		drama.Cover != "https://pic.example/cover.jpg" || drama.Views != "1.2万次播放" || drama.OnlineDate != "2026-10-23" {
		t.Fatalf("new huangguo detail failed: %+v %d legacy=%t err=%v", drama, len(chapters), sawLegacy, err)
	}
	if chapters[0].PageURL != "https://huangguoai.com/video/10219/" || chapters[1].PageURL != "https://huangguoai.com/video/10219/ep-2/" {
		t.Fatalf("new huangguo chapter URLs were not canonicalized: %+v", chapters[:2])
	}
	media, err := d.resolveProviderMedia(context.Background(), Task{DramaID: drama.ID, Chapter: chapters[0], Index: 1})
	if err != nil || !strings.HasPrefix(media.Playlist, "#EXTM3U") || !strings.Contains(media.URL, "one.m3u8") {
		t.Fatalf("new huangguo playback failed: %+v %v", media, err)
	}
}
