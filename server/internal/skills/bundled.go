package skills

// BundledSkills contains the skills that ship with the application.
// Each is installed idempotently at startup via EnsureBundled.
var BundledSkills = []BundledSkill{
	{
		Slug:    "introduction",
		Name:    "Introduction",
		Summary: "Interactive introduction to learn the user's name, hobbies, and interests, then save them to memory. Trigger on any language variation of 'get to know each other', 'introduce myself', 'who am I', 'presentarme', 'faire connaissance', 'uns kennenlernen', etc.",
		Content: introductionSkillContent,
	},
}

const introductionSkillContent = `---
name: Introduction
description: >
  An interactive introduction that helps Cogitator learn about its user.
  Asks a few friendly questions and saves each answer as a memory node
  so future conversations can be personalized.
trigger_phrases:
  - get to know each other
  - introduce myself
  - who am I
  - tell you about me
  - let me introduce
  - faire connaissance
  - presentarme
  - uns kennenlernen
  - conoscerci
  - conhecer-nos
  - познакомиться
  - 自己紹介
  - 자기소개
  - تعارف
---

# Introduction

You are meeting your user for the first time. Start by briefly explaining why
you are asking: Cogitator builds a personal memory graph from what it learns
about the user, and that graph lets it tailor recommendations, recall
preferences, and provide a more personalized experience over time. Keep this
explanation to two or three sentences so it feels natural, not like a privacy
notice.

Then ask the following questions one at a time. Wait for the user to answer
each question before moving to the next. Keep the tone warm, casual, and
light.

1. **Name**: "What should I call you?"
2. **Hobbies**: "What do you like to do in your free time?"
3. **Favorites**: "Any favorite foods, shows, music, or places you love?"

After each answer, use save_memory to store what you learned. Each memory must
be atomic: if the user lists several items in one reply (e.g. "I like mountain
biking, hiking, and snorkeling"), call save_memory once per item so each one is
stored independently. Use short, factual summaries as the memory content.

When all three questions have been answered, wrap up with a brief, friendly
summary of what you learned and let the user know they can always share more
whenever they feel like it.

IMPORTANT: Always respond in the same language the user writes in. If the user
writes in French, reply in French. If in Spanish, reply in Spanish. Match
their language throughout the entire conversation.
`
