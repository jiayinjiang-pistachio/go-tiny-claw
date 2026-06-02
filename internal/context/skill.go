package context

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Skill 定义了从 SKILL.md 中解析出的标准化技能结构
type Skill struct {
	Name string
	Description string
	Body string  // markdown 正文命令
}

// SkillLoader 负责从本地文件系统中加载符合规范的技能模板
type SkillLoader struct {
	workdir string
}

func NewSkillLoader(workdir string) *SkillLoader {
	return &SkillLoader{workdir: workdir}
}

// LoadAll 扫描 .claw/skills 目录，解析所有 SKILL.md，并格式化字符串准备注入 Context
func (s *SkillLoader) LoadAll() string {
	skillBaseDir := filepath.Join(s.workdir, ".claw", "skills")

	// 如果目录不存在，说明当前工作区没有配置技能，静默返回
	if _, err := os.Stat(skillBaseDir); os.IsNotExist(err) {
		return ""
	}

	var skillsBuilder strings.Builder
	skillsBuilder.WriteString("\n### 可用专业技能（Agent Skills）\n")
	skillsBuilder.WriteString("以下是你拥有的标准化外挂技能，请在符合 description 描述的场景下严格遵循其正文指令：\n\n")

	// 遍历查找 SKILL.md
	err := filepath.WalkDir(skillBaseDir, func (path string, d fs.DirEntry, err error) error  {
		if err != nil {
			return err
		}

		// 仅处理名为 SKILL.md 的文件
		if !d.IsDir() && d.Name() == "SKILL.md" {
			content, err := os.ReadFile(path)
			if err == nil {
				skill := parseSkillMD(string(content))

				// 将解压后的技能按结构注入
				skillsBuilder.WriteString(fmt.Sprintf("### 技能名称：%s\n", skill.Name))
				skillsBuilder.WriteString(fmt.Sprintf("**触发条件**：%s\n\n", skill.Description))
				skillsBuilder.WriteString(fmt.Sprintf("**执行指南**：\n"))
				skillsBuilder.WriteString(skill.Body)
				skillsBuilder.WriteString("\n\n---\n\n")
			}
		}

		return nil
	})

	if err != nil || skillsBuilder.Len() < 100 {
		return "" // 如果加载失败或内容过少，说明没有有效技能，返回空字符串
	}

	return skillsBuilder.String()
}

// parseSkillMD 极简解析带有 YAML Frontmatter 的 markdown 内容
func parseSkillMD(content string) Skill {
	skill := Skill{
		Name: "Unknown Skill",
		Description: "No description Provided.",
		Body: content, // 默认整个内容作为 Body
	}

	// 简单解析 YAML Frontmatter (以 --- 包裹)
	if strings.HasPrefix(content, "---\n") || strings.HasPrefix(content, "---\r\n") {
		parts := strings.SplitN(content, "---", 3)
		if len(parts) == 3 {
			frontmatter := parts[1]
			skill.Body = strings.TrimSpace(parts[2])

			// 逐行提取 metadata
			lines := strings.Split(frontmatter, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "name:") {
					skill.Name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
				} else if strings.HasPrefix(line, "description:") {
					skill.Description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
				}
			}
		}
	}

	return skill
}