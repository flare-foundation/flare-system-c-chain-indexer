CREATE DATABASE  IF NOT EXISTS `flare_ftso_indexer_songbird`;

USE flare_ftso_indexer_songbird;
DROP TABLE IF EXISTS `states`;
CREATE TABLE `states` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `name` varchar(50) NOT NULL,
  `next_db_index` int unsigned DEFAULT NULL,
  `last_chain_index` int unsigned DEFAULT NULL,
  PRIMARY KEY (`id`)
);
INSERT INTO `states` (`name`, `next_db_index`, `last_chain_index`, 'first_db_index')
VALUES ('ftso_indexer', 0, 0, 0);

GRANT ALL PRIVILEGES ON `flare_ftso_indexer_songbird`.* TO 'indexeruser'@'%';
