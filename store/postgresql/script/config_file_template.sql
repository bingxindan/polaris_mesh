/*
 Navicat Premium Data Transfer

 Source Server         : dev-postgresql
 Source Server Type    : PostgreSQL
 Source Server Version : 90224 (90224)
 Source Host           : 192.168.31.19:5432
 Source Catalog        : polaris_server
 Source Schema         : public

 Target Server Type    : PostgreSQL
 Target Server Version : 90224 (90224)
 File Encoding         : 65001

 Date: 13/06/2023 01:41:10
*/


-- ----------------------------
-- Table structure for config_file_template
-- ----------------------------
DROP TABLE IF EXISTS "public"."config_file_template";
CREATE TABLE "public"."config_file_template" (
  "id" int8 NOT NULL,
  "name" varchar(128) COLLATE "pg_catalog"."default" NOT NULL,
  "content" text COLLATE "pg_catalog"."default" NOT NULL,
  "format" varchar(16) COLLATE "pg_catalog"."default" DEFAULT 'text'::character varying,
  "comment" varchar(512) COLLATE "pg_catalog"."default" DEFAULT NULL::character varying,
  "create_time" timestamp(6) NOT NULL DEFAULT now(),
  "create_by" varchar(32) COLLATE "pg_catalog"."default" DEFAULT NULL::character varying,
  "modify_time" timestamp(6) NOT NULL DEFAULT now(),
  "modify_by" varchar(32) COLLATE "pg_catalog"."default" DEFAULT NULL::character varying
)
;
ALTER TABLE "public"."config_file_template" OWNER TO "postgres";

-- ----------------------------
-- Records of config_file_template
-- ----------------------------
BEGIN;
INSERT INTO "public"."config_file_template" ("id", "name", "content", "format", "comment", "create_time", "create_by", "modify_time", "modify_by") VALUES (1, 'spring-cloud-gateway-braining', '{
    "rules":[
        {
            "conditions":[
                {
                    "key":"${http.query.uid}",
                    "values":["10000"],
                    "operation":"EQUALS"
                }
            ],
            "labels":[
                {
                    "key":"env",
                    "value":"green"
                }
            ]
        }
    ]
}
', 'json', 'Spring Cloud Gateway  染色规则', '2023-06-13 01:40:24', 'polaris', '2023-06-13 01:40:34', 'polaris');
COMMIT;

-- ----------------------------
-- Primary Key structure for table config_file_template
-- ----------------------------
ALTER TABLE "public"."config_file_template" ADD CONSTRAINT "config_file_template_pkey" PRIMARY KEY ("id");
