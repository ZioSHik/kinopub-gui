// The kino.pub catalog categories, mirroring the site's category sidebar.
//
// kino.pub's sidebar is a curated mix: most entries are real API *content types*
// (movie, serial, tvshow, concert, documovie, docuserial), while Anime and Sport
// are *genre shortcuts* that span several types (verified against the live API:
// anime = genre 25; sport = genres 20 (movie) + 71 (docu)). Genres themselves are
// type-scoped, so each type category carries the type whose genre list to offer
// as sub-genres; genre categories are already a genre and offer no sub-genres.
import {
  Clapperboard,
  Disc3,
  FileText,
  FileVideo,
  MonitorPlay,
  Music,
  Tv,
  Volleyball,
  type LucideIcon,
} from "lucide-react";

export interface Category {
  key: string; // stable id stored in FilterState.category
  label: string; // i18n source string
  icon: LucideIcon;
  type: string; // kino.pub content type ("" for genre-based categories)
  genre: string; // fixed genre id / CSV for genre-based categories ("" otherwise)
  genreType: string; // type whose genre list to show as sub-genres ("" = none)
}

export const CATEGORIES: Category[] = [
  { key: "movie", label: "Movies", icon: Clapperboard, type: "movie", genre: "", genreType: "movie" },
  { key: "serial", label: "Series", icon: MonitorPlay, type: "serial", genre: "", genreType: "serial" },
  { key: "anime", label: "Anime", icon: Disc3, type: "", genre: "25", genreType: "" },
  { key: "concert", label: "Concerts", icon: Music, type: "concert", genre: "", genreType: "concert" },
  { key: "documovie", label: "Documentaries", icon: FileVideo, type: "documovie", genre: "", genreType: "documovie" },
  { key: "docuserial", label: "Docuseries", icon: FileText, type: "docuserial", genre: "", genreType: "docuserial" },
  { key: "tvshow", label: "TV shows", icon: Tv, type: "tvshow", genre: "", genreType: "tvshow" },
  { key: "sport", label: "Sport", icon: Volleyball, type: "", genre: "20,71", genreType: "" },
];

export const categoryByKey = (key: string): Category | undefined => CATEGORIES.find((c) => c.key === key);
